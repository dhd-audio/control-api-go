# Go Client Library for the DHD Control API

This package provides a Go client for  
[DHD Control API](https://developer.dhd.audio/docs/API/control-api/).

We built this library to help our customers kickstart their API projects with the DHD Control API, providing a simple and efficient way to interact with DHD devices.

Control API requires DHD XC3/XD3/X3 IP Cores with a minimum firmware version 10.3.

[![Go Reference](https://pkg.go.dev/badge/github.com/dhd-audio/control-api-go.svg)](https://pkg.go.dev/github.com/dhd-audio/control-api-go)

## Quick Start

### Import Library

````go
import controlapi "github.com/dhd-audio/control-api-go"
````

### Create Client

First prepare the `ClientObjects`, then pass them to `NewClient`:

````go
options := controlapi.ClientOptions{
    URL:         "https://[DNS Name or IP]", 
    Token:       "mysecrettoken",
    InsecureTLS: false,
}
client := controlapi.NewClient(&options)
````

Replace `URL` and `Token` by your actual values.

The `InsecureTLS` option will make the client ignore invalid TLS certificates.

## REST API

Get a single value, or a subtree:

````go
result, err := client.Get("/audio/mixers/0/faders/0")
````

Get the names of all children of a particular node:

````go
result, err := client.GetFlat("/audio/mixers/0/faders/0")
````

Set a value:

```go
err := client.Set("/audio/mixers/0/faders/0/pfl1", "true")
````

## WebSocket API

The `NewWebSocket` method establishes an authenticated connection
to the WebSocket API, and parses incoming messages, turning them
into event structures. You must provide a channel that receives
these events. The channel will be closed when the connection is
terminated, i.e. no more message will be received.

If `nil` is provided as the channel, no event notifications will
be sent, and only the synchronous request methods can be used
(see below).

Example code:

````go
// Create event channel
ch := make(chan any, 100)

// Open WebSocket and authenticate
ws, err := client.NewWebSocket(ch)
if err != nil {
    // handle error, e.g. authentication error
}
defer ws.Close()

// Process events received from WebSocket
for evt := range ch {
    e := evt.(type) {
    case controlapi.NodeValueEvent:
        fmt.Printf("Node %s is now %v\n", e.Path, e.Value)
    // ...
    // Handle other events
    // ...
    }
}
````

See the "Events" section below for the available events.

### Synchronous Requests

While the WebSocket API is asynchronous by nature, the WebSocket object
provides a range of methods that send a specific request, tagged with 
a random message ID, and wait for the reply from the device. They will
block until the reply is received, or a timeout occurs.

Get a single value, or a subtree:

````go
val, err := ws.Get("/audio/mixers/0/faders/0/pfl1")
````

Set a node value, receiving the new (echoed) value from the device:

````go
val, err := ws.Set("/audio/mixers/0/faders/0/pfl1", true)
````

Subscribe to a node or subtree, asynchronously receiving changes
through `NodeValueEvent` events:

````go
err := ws.Subscribe("/audio/mixers/0")
````

Unsubscribe again:

````go
err := ws.Unsubscribe("/audio/mixers/0")
````

Additional methods are available for RPC requests. Please see the
package documentation for details.

### Raw Synchronous Requests

In Control API, a request is a JSON object that follows a particular structure.
Interally, we define the type `Request` to represent an arbitrary request.
When being sent over the WebSockt, the request object is marshalled to JSON.

````go
type Request map[string]any
````

There are several functions (in [requests.go](requests.go)) available
that pre-populate requests with the required fields. For example,
`NewGetRequest` sets the `method` and `path` fields in the request
object:

````go 
func NewGetRequest(path string) Request {
	return Request{
		"method": "get",
		"path":   path,
	}
}
````

But of course you can also build your own request object this way,
for example to use functionality that had not been available by the
time this version of the package was written.

The WebSocket object provides two methods to send raw requests
synchronously, i.e. tag them with a message ID and wait for the 
reply from the device. It returns the entire reply from the device,
unmarshalled into a `WebSocketMessage` object:

````go
req := controlapi.NewGetRequest("/audio/mixers/0/faders/0/fader")
msg, err := ws.RequestAndWait(req)
````

Usually you would be interested in the contents of the "payload" field
of the reply. Using `RequestAndWaitForReply`, you can conveniently
perform the request, wait for the reply, and extract the payload for 
further processing:

````go
req := controlapi.NewGetRequest("/audio/mixers/0/faders/0/fader")
payload, err := ws.RequestAndWaitForPayload(req)
````

### Asynchronous Raw Requests

Raw requests can also be sent asynchronously - the
`Send()` function writes the request message to the WebSocket, but
returns immediately without waiting for a reply or checking
for errors:

````go
req := controlapi.NewGetRequest("/audio/mixers/0/faders/0/fader")
ws.Send(req) // no return value
````

Replies to this kind of raw requests will be received through the
event channel, and must be processed manually.

### Events

If a `chan any` is supplied during the creation of the WebSocket,
the `Run()` method will continously send notifications to that
channel. The values sent to that channel are the structs defined in
[events.go](events.go).

In particular, the following events will occur:

* `MessageEvent`: Raw JSON message (unmarshalled to `map[string]any`)
  as received from the WebSocket.
* `WebSocketMessage` (defined in [websocket.go](websocket.go)) - the same 
  as `MessageEvent`, but the message is already unmarshalled into a 
  `WebSocketMessage` object.
* `NodeValueEvent`: This event reports the value of a node in the parameter
  tree. It occurs when the reply to a "get" request is received, as well
  as spontaneously when the device reports a change of a node in any
  of the subscribed sub-trees. The event only carries a single node and value. 
  If the device reports multiple nodes and values in a single WebSocket message, 
  a separate `NodeValueEvent` is generated for each single node.

See [events.go](events.go) for the full reference of all possible
events and their fields.

## Contributing

If you have added new features or fixed bugs, feel free to create a pull request.

## License

Copyright (c) 2025 DHD audio GmbH.

Licensed under MIT License. See `LICENSE` for the full licensing terms.
