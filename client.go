// Package dhdcontrolapi provides a client for the DHD control API.
//
// Both the WebSocket and REST API are supported.
package dhdcontrolapi

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	neturl "net/url"
	"time"
)

// ClientOptions represents the options for a ControlAPI client.
type ClientOptions struct {
	// Base URL of the DHD control API, including the protocol and port, but exluding the /api/ws or /api/rest part.
	// The URL may contain basic authentication credentials, like http://user:pass@host:port.
	URL string

	// Token to authenticate with the DHD control API.
	Token string

	// InsecureTLS allows to disable TLS certificate verification.
	InsecureTLS bool

	// The duration that the WebSocket request methods wait for a reply before timing out.
	// If set to 0, the default timeout of 5 seconds will be used.
	WebSocketReplyTimeout time.Duration
}

// Client represents a connection to the DHD control API.
type Client struct {
	options    ClientOptions
	httpclient *http.Client
}

// NewClient creates a new Control API client.
func NewClient(options *ClientOptions) *Client {
	// Create HTTP client with optional TLS verification disabled
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: options.InsecureTLS},
	}
	httpclient := &http.Client{Transport: tr}

	return &Client{
		options:    *options,
		httpclient: httpclient,
	}
}

// Options returns the options of the client.
func (c *Client) Options() ClientOptions {
	return c.options
}

// NewWebSocket creates a new WebSocket connection to the DHD Control API.
//
// Any error occuring during connection or authentication phase will be reported
// through the returned error.
//
// If an (optional) event channel is supplied, the WebSocket will start sending
// notifications to that channel whenver an event is received and parsed.
//
// The channel will be closed when the connection is terminated.
//
// The caller must call Close on the WebSocket when done.
func (c *Client) NewWebSocket(eventChannel chan any) (*WebSocket, error) {
	// Create a new WebSocket connection
	ws := NewWebSocket(&c.options)

	// Connect the WebSocket
	err := ws.Connect()
	if err != nil {
		return nil, err
	}

	// Run the WebSocket receive loop in a separate goroutine
	go ws.Run(eventChannel)

	return ws, nil
}

// Get performs a "get" request on the specified path.
func (c *Client) Get(path string) (any, error) {

	// Set query parameters
	vals := neturl.Values{}
	vals.Add("token", c.options.Token)

	// Construct URL
	url := c.options.URL + "/api/rest/" + path + "?" + vals.Encode()

	// Perform HTTP request
	resp, err := c.httpclient.Get(url)
	if err != nil {
		return nil, err
	}

	// Check if the request was successful
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("HTTP request failed: " + resp.Status)
	}

	// Umarshal the response
	var data any
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// GetFlat performs a "get" request on the specified path with the "flat" option,
// which returns a list of all nodes under the specified path.
func (c *Client) GetFlat(path string) ([]string, error) {

	// Set query parameters
	vals := neturl.Values{}
	vals.Add("token", c.options.Token)
	vals.Add("flat", "true")

	// Construct URL
	url := c.options.URL + "/api/rest/" + path + "?" + vals.Encode()

	// Perform HTTP request
	resp, err := c.httpclient.Get(url)
	if err != nil {
		return nil, err
	}

	// Check if the request was successful
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("HTTP request failed: " + resp.Status)
	}

	// Unmarshal the response
	var data []string
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// Set performs a "set"	request on the specified path, setting the given value.
// It returns the response from the server, which is the new value of the node.
func (c *Client) Set(path string, value string) (string, error) {
	// Set query parameters
	vals := neturl.Values{}
	vals.Add("token", c.options.Token)
	vals.Add("set", value)

	// Construct URL
	url := c.options.URL + "/api/rest/" + path + "?" + vals.Encode()

	// Perform HTTP request
	resp, err := c.httpclient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Check if the request was successful
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("HTTP request failed: " + resp.Status)
	}

	// Read response body into string
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(bodyBytes), nil
}
