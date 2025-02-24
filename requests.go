package dhdcontrolapi

// Request represents a request to the DHD Control WebSocket API,
// which is generally a map of string keys to any values,
// marshalled to a JSON object when sent through the WebSocket.
type Request map[string]any

// WithMsgID creates a new request by adding or setting the message ID.
func (r Request) WithMsgID(msgID string) Request {
	r["msgID"] = msgID
	return r
}

// NewAuthRequest creates an authentication request.
func NewAuthRequest(token string) Request {
	return Request{
		"method": "auth",
		"token":  token,
	}
}

// NewGetRequest creates a get request.
func NewGetRequest(path string) Request {
	return Request{
		"method": "get",
		"path":   path,
	}
}

// NewSetRequest creates a set request.
func NewSetRequest(path string, payload any) Request {
	return Request{
		"method":  "set",
		"path":    path,
		"payload": payload,
	}
}

// NewSubscribeRequest creates a subscribe request.
func NewSubscribeRequest(path string) Request {
	return Request{
		"method": "subscribe",
		"path":   path,
	}
}

// NewUnsubscribeRequest creates an unsubscribe request.
func NewUnsubscribeRequest(path string) Request {
	return Request{
		"method": "unsubscribe",
		"path":   path,
	}
}

// NewRPCRequest creates an RPC request with the given payload.
func NewRPCRequest(payload any) Request {
	return Request{
		"method":  "rpc",
		"payload": payload,
	}
}

// NewExtListRegisterRequest creates an RPC request to register an external button list.
func NewExtListRegisterRequest(list int) Request {
	return NewRPCRequest(map[string]any{
		"method": "registerextlist",
		"params": map[string]any{
			"list": list,
		},
	})
}

// NewExtListUnregisterRequest creates an RPC request to unregister an external button list.
func NewExtListUnregisterRequest(list int) Request {
	return NewRPCRequest(map[string]any{
		"method": "unregisterextlist",
		"params": map[string]any{
			"list": list,
		},
	})
}

// ExtListEntry represents an entry in an external button list
type ExtListEntry struct {
	Label      string `json:"label"`
	Color      string `json:"color,omitempty"`
	Background bool   `json:"background,omitempty"`
	Disabled   bool   `json:"disabled,omitempty"`
	Pagination bool   `json:"pagination,omitempty"`
}

// NewSetExtListRequest creates an RPC request to update an external button list.
func NewSetExtListRequest(list int, entries []ExtListEntry, selectedEntry int) Request {

	// Create the parameters for the button list
	params := map[string]any{
		"list":    list,
		"entries": entries,
	}

	// Add the selected entry if it is valid
	if selectedEntry >= 0 {
		params["select"] = selectedEntry
	}

	// Create the RPC request payload
	payload := map[string]any{
		"method": "setextlist",
		"params": params,
	}

	// Create and return the RPC request
	return NewRPCRequest(payload)
}

// NewGetAccessRequest creates an RPC request to get and subscribe to the access state.
func NewGetAccessRequest() Request {
	return NewRPCRequest(map[string]any{
		"method": "getaccess",
	})
}
