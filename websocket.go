package dhdcontrolapi

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	neturl "net/url"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/mitchellh/mapstructure"
)

// Constants
const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// defaultReplyTimeout is the default time to wait for a reply to a request
	defaultReplyTimeout = 5 * time.Second
)

var (
	// ErrUnexpectedAuthReply is returned when an unexpected reply to an auth request is received.
	ErrUnexpectedAuthReply = errors.New("unexpected reply to auth request")

	// ErrAuthFailed is returned when authentication failed.
	ErrAuthFailed = errors.New("authentication failed")

	// ErrAwaitTimeout is returned when a reply to a request was not received in time.
	ErrAwaitTimeout = errors.New("timeout awaiting reply")
)

// WebSocketMessageError is the "error" field in a WebSocket message received from the API.
//
// It also implements the Error interface, and is being used to signal errors in the various
// synchronous request methods.
type WebSocketMessageError struct {
	Code    int    `mapstructure:"code"`
	Message string `mapstructure:"message"`
}

// Error implements the error interface
func (e WebSocketMessageError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.Code, e.Message)
}

// WebSocketMessage is an unmarshalled message received from the API over WebSocket.
//
// Structs of this type are sent to the event channel whenever a well-formed message is received.
type WebSocketMessage struct {
	MsgID   string                `mapstructure:"msgID"`
	Method  string                `mapstructure:"method"`
	Path    string                `mapstructure:"path"`
	Payload any                   `mapstructure:"payload"`
	Success bool                  `mapstructure:"success"`
	Error   WebSocketMessageError `mapstructure:"error"`
}

// webSocketRPCReplyPayload is the unmarshalled payload of an RPC reply message.
type webSocketRPCReplyPayload struct {
	Method string `mapstructure:"method"`
	Result any    `mapstructure:"result"`
}

// webSocketRPCUpdatePayload is the unmarshalled payload of an RPC update message.
type webSocketRPCUpdatePayload struct {
	Method string         `mapstructure:"method"`
	Type   string         `mapstructure:"type"`
	Params map[string]any `mapstructure:"params"`
}

// WebSocket represents a connection to a DHD device via the WebSocket API.
type WebSocket struct {
	options             ClientOptions
	conn                *websocket.Conn
	sendQueue           chan (Request)
	awaitedReplies      map[string]chan WebSocketMessage
	awaitedRepliesMutex *sync.Mutex
}

// NewWebSocket creates a new WebSocket connection.
// The url must be the full URL to the WebSocket API, including the protocol and port, and the "/api/ws" part.
// http/https are automatically converted to ws/wss.
func NewWebSocket(options *ClientOptions) *WebSocket {
	return &WebSocket{
		options:             *options,
		sendQueue:           make(chan Request, 100),
		awaitedReplies:      make(map[string]chan WebSocketMessage),
		awaitedRepliesMutex: &sync.Mutex{},
	}
}

// Connect establishes a connection to the DHD device, and authenticates if a token is configured.
func (ws *WebSocket) Connect() error {

	// Parse URL
	u, err := neturl.Parse(ws.options.URL + "/api/ws")
	if err != nil {
		return err
	}

	// Change scheme to WS
	if u.Scheme == "http" {
		u.Scheme = "ws"

	} else if u.Scheme == "https" {
		u.Scheme = "wss"
	}

	authHeader := http.Header{}

	// If user/password is given, create basic auth header, and strip it from URL
	if u.User != nil && u.User.Username() != "" {
		username := u.User.Username()
		password, isset := u.User.Password()
		if isset {
			authHeader.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
		}
		u.User = nil
	}

	// Create dialer
	dialer := *websocket.DefaultDialer
	dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: ws.options.InsecureTLS}

	// Establish connection
	c, _, err := dialer.Dial(u.String(), authHeader)
	if err != nil {
		return err
	}

	// If a token is configured, authenticate using that token
	if ws.options.Token != "" {
		// Send auth message
		c.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err := c.WriteJSON(NewAuthRequest(ws.options.Token))
		if err != nil {
			c.Close()
			return err
		}

		// Wait for reply
		var msg map[string]any
		c.SetReadDeadline(time.Now().Add(10 * time.Second))
		err = c.ReadJSON(&msg)
		if err != nil {
			c.Close()
			return err
		}

		// Reset deadline
		c.SetReadDeadline(time.Time{})

		// Parse reply
		if msg["method"] != "auth" {
			c.Close()
			return ErrUnexpectedAuthReply
		}

		// Check if auth was successful
		if msg["success"] != true {
			c.Close()
			return ErrAuthFailed
		}
	}

	ws.conn = c

	return nil
}

// Close closes the connection to the DHD device.
func (ws *WebSocket) Close() error {
	close(ws.sendQueue)
	return ws.conn.Close()
}

// Send sends a request to the DHD device over the WebSocket connection.
// The request will be marshalled to JSON.
//
// The message will be sent asynchronously, and the function returns immediately.
// Any reply from the device will be handled by the Run function and
// sent to the event channel.
func (ws *WebSocket) Send(request Request) {
	ws.sendQueue <- request
}

// RequestAndWait sends a request to the DHD device, and waits for a reply.
//
// If no reply is received within awaitReplyTimeout, an error is returned.
func (ws *WebSocket) RequestAndWait(request Request) (WebSocketMessage, error) {
	// Generate a new message ID
	msgid := uuid.New().String()

	// Create a channel through which we will receive the reply
	replyChannel := make(chan WebSocketMessage, 1)

	// Add the message ID to the request
	request = request.WithMsgID(msgid)

	// Add the channel to the awaited replies map
	ws.awaitedRepliesMutex.Lock()
	ws.awaitedReplies[msgid] = replyChannel
	ws.awaitedRepliesMutex.Unlock()

	// At the end of this function, we must remove the channel from the awaited replies map
	defer func() {
		ws.awaitedRepliesMutex.Lock()
		delete(ws.awaitedReplies, msgid)
		ws.awaitedRepliesMutex.Unlock()
	}()

	// Send the request
	ws.Send(request)

	// Use the configured reply timeout, or the default
	timeout := ws.options.WebSocketReplyTimeout
	if timeout == 0 {
		timeout = defaultReplyTimeout
	}

	// Wait for the reply, or timeout
	select {
	case reply := <-replyChannel:
		if !reply.Success {
			return WebSocketMessage{}, reply.Error
		}
		return reply, nil
	case <-time.After(timeout):
		return WebSocketMessage{}, ErrAwaitTimeout
	}
}

// RequestAndWaitForPayload sends a request to the DHD device, and waits for a reply.
// On success, the payload will be extracted and returned.
//
// If no reply is received within awaitReplyTimeout, an error is returned.
func (ws *WebSocket) RequestAndWaitForPayload(request Request) (any, error) {
	r, err := ws.RequestAndWait(request)
	if err != nil {
		return nil, err
	}
	return r.Payload, nil
}

// Get sends a "get" request to the DHD device.
//
// This method is synchronous. It sends the request, waits for the reply,
// and returns the requested value, or an error.
func (ws *WebSocket) Get(path string) (any, error) {
	return ws.RequestAndWaitForPayload(NewGetRequest(path))
}

// Set sends a "set" request to the DHD device.
// It returns the new value as confirmed by the device, or an error.
//
// This method is synchronous. It sends the request, waits for the reply,
// and returns the requested value, or an error.
func (ws *WebSocket) Set(path string, payload any) (any, error) {
	return ws.RequestAndWaitForPayload(NewSetRequest(path, payload))
}

// Subscribe sends a "subscribe" request to the DHD device,
// and waits for confirmation.
//
// The device will start sending events when any parameters
// at or below the specified path changes, until the
// connection is terminated, or an "unsubscribe" request is sent.
//
// Received events will be sent to the event channel
// as NodeValueEvent.
//
// This method is synchronous. It sends the request and waits for the reply.
func (ws *WebSocket) Subscribe(path string) error {
	_, err := ws.RequestAndWait(NewSubscribeRequest(path))
	return err
}

// Unsubscribe sends an "unsubscribe" request to the DHD device.
//
// This method is synchronous. It sends the request and waits for the reply.
func (ws *WebSocket) Unsubscribe(path string) error {
	_, err := ws.RequestAndWait(NewUnsubscribeRequest(path))
	return err
}

// RPC sends an RPC request to the DHD device.
//
// This method is synchronous. It sends the request and waits for the reply.
func (ws *WebSocket) RPC(payload any) (any, error) {
	return ws.RequestAndWaitForPayload(NewRPCRequest(payload))
}

// RegisterExtList sends a "registerextlist" RPC request to the DHD device.
//
// This method is synchronous. It sends the request and waits for the reply.
func (ws *WebSocket) RegisterExtList(list int) error {
	_, err := ws.RequestAndWait(NewExtListRegisterRequest(list))
	return err
}

// UnregisterExtList sends an "unregisterextlist" RPC request to the DHD device.
//
// This method is synchronous. It sends the request and waits for the reply.
func (ws *WebSocket) UnregisterExtList(list int) error {
	_, err := ws.RequestAndWait(NewExtListUnregisterRequest(list))
	return err
}

// SetExtList sends a "setextlist" RPC request to the DHD device.
//
// This will populate the external button list with the specified entries,
// and select the specified entry.
//
// This method is synchronous. It sends the request and waits for the reply.
func (ws *WebSocket) SetExtList(list int, entries []ExtListEntry, selectedEntry int) error {
	_, err := ws.RequestAndWait(NewSetExtListRequest(list, entries, selectedEntry))
	return err
}

// GetAccess sends a "getaccess" RPC request to the DHD device.
//
// It returns the current access state of the device, or an error.
//
// The command will also enable subscription for access state changes,
// which are notified through the event channel as AccessEvent.
//
// This method is synchronous. It sends the request and waits for the reply.
func (ws *WebSocket) GetAccess() (any, error) {
	return ws.RequestAndWaitForPayload(NewGetAccessRequest())
}

// nodeValueReceived parses data received in a "get" or "update" answer, and passes
// the values to the event channel.
// In case of a "get" answer, which contains a "path" value, that path is used
// as the prefix for all parsed paths.
// For "update" events, the pathPrefix is empty.
func (ws *WebSocket) nodeValueReceived(pathPrefix string, value any, eventChannel chan any) {

	// Helper function that parses the payload
	var traverse func(path string, value any)
	traverse = func(path string, value any) {

		// Object? Traverse it.
		if data, ok := value.(map[string]any); ok {
			for node, val := range data {
				traverse(path+"/"+node, val)
			}
			return
		}

		// Any other value (string, number, bool, ... - also array) - trigger event
		eventChannel <- NodeValueEvent{
			Path:  path,
			Value: value,
		}
	}

	// Start traversing the root
	traverse(pathPrefix, value)
}

// accessStateReceived parses the payload received by a getaccess RPC reply or notification
func (ws *WebSocket) accessStateReceived(data any, eventChannel chan any) {

	// Parse payload into convenient struct
	var payload struct {
		Mixer map[string]struct {
			AccessGroup map[string]int
		}
	}
	err := mapstructure.Decode(data, &payload)
	if err != nil {
		return
	}

	// Iterate through mixers
	for m, data := range payload.Mixer {
		mixer, err := strconv.Atoi(m)
		if err != nil {
			continue
		}

		// Iterate through access groups
		for ag, fader := range data.AccessGroup {
			accessGroup, err := strconv.Atoi(ag)
			if err != nil {
				continue
			}

			eventChannel <- AccessEvent{
				Mixer:       mixer,
				AccessGroup: accessGroup,
				Fader:       fader,
			}
		}
	}
}

// writeLoop pumps pending messages from the sendQueue to the socket
func (ws *WebSocket) writeLoop() {
	// Create a ticker that will send a ping at regular intervals
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		ws.conn.Close()
	}()

	for {
		select {

		case msg, more := <-ws.sendQueue:

			// Send queue closed? Send CloseMessage and exit
			if !more {
				ws.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}

			// Send message
			ws.conn.SetWriteDeadline(time.Now().Add(writeWait))
			err := ws.conn.WriteJSON(msg)
			if err != nil {
				return
			}

		case <-ticker.C:

			// Send ping message
			ws.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := ws.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		}
	}
}

// Run reads messages from the websocket connection and parses them.
//
// If an event channel is supplied, the raw and parsed messages will be sent to that channel.
// The channel will be closed when the connection is closed.
//
// If no event channel is supplied, the function will only handle awaited replies.
func (ws *WebSocket) Run(eventChannel chan any) {
	// Close the event channel when this function exits
	defer func() {
		if eventChannel != nil {
			close(eventChannel)
		}
	}()

	// Start the write loop
	go ws.writeLoop()

	// Use a read timeout of the pong interval
	ws.conn.SetReadDeadline(time.Now().Add(pongWait))

	// Whenever a pong is received, update deadline
	ws.conn.SetPongHandler(func(string) error {
		ws.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Read messages in a separate goroutine
	readQueue := make(chan map[string]any, 100)
	go func() {
		for {
			var msg map[string]any
			err := ws.conn.ReadJSON(&msg)
			if err != nil {
				close(readQueue)
				return
			}
			readQueue <- msg
		}
	}()

	// Main loop
	for {

		select {
		case msg, ok := <-readQueue:
			// Channel closed? Exit
			if !ok {
				return
			}

			// Send raw message to event handler
			eventChannel <- MessageEvent{Message: msg}

			// Decode message
			var m WebSocketMessage
			err := mapstructure.Decode(msg, &m)
			if err != nil {
				break
			}

			// Check if there is a message ID, and if so, check if it's an awaited reply
			if m.MsgID != "" {
				ws.awaitedRepliesMutex.Lock()
				replyChannel, ok := ws.awaitedReplies[m.MsgID]
				ws.awaitedRepliesMutex.Unlock()
				// If it is, send the reply to the channel
				if ok {
					replyChannel <- m
				}
			}

			// The following must only be done if an event channel was supplied
			if eventChannel == nil {
				break
			}

			// Send parsed message to event handler
			eventChannel <- m

			// Now we handle some specific message types and generate additional events.
			switch m.Method {

			// Tree reply
			case "get":
				ws.nodeValueReceived(m.Path, m.Payload, eventChannel)

			// Tree update notifiction
			case "update":
				ws.nodeValueReceived("", m.Payload, eventChannel)

			// RPC reply
			case "rpc":

				// Decode RPC reply payload
				var payload webSocketRPCReplyPayload
				err := mapstructure.Decode(m.Payload, &payload)
				if err != nil {
					break
				}

				switch payload.Method {

				// Access
				case "getaccess":
					ws.accessStateReceived(payload.Result, eventChannel)

				}

			// RPC update notification
			case "event":

				// Decode RPC update payload
				var payload webSocketRPCUpdatePayload
				err := mapstructure.Decode(m.Payload, &payload)
				if err != nil {
					break
				}

				switch payload.Type {

				// External list button notification
				case "extlistnotified":
					eventChannel <- ExtListNotifiedEvent{
						List:  int(payload.Params["list"].(float64)),
						Entry: int(payload.Params["entry"].(float64)),
						On:    payload.Params["on"] == float64(1),
					}

				// Access changed
				case "getaccess":
					ws.accessStateReceived(payload.Params, eventChannel)

				}
			}
		}
	}
}
