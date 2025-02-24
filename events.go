package dhdcontrolapi

// MessageEvent is a basic event that carries the payload of a WebSocket message
type MessageEvent struct {
	Message any
}

// NodeValueEvent is a WebSocket event that occurs when a node value changes,
// or as a reply to a get request. If the get reply (or update) contains values
// for multiple nodes, each node will be reported as a separate event.
type NodeValueEvent struct {
	Path  string
	Value any
}

// ExtListNotifiedEvent is a WebSocket event that occurs when the user presses or releases an external button.
type ExtListNotifiedEvent struct {
	List  int
	Entry int
	On    bool
}

// AccessEvent is a WebSocket event that occurs when the access state of a mixer changes.
type AccessEvent struct {
	Mixer       int
	AccessGroup int
	Fader       int
}
