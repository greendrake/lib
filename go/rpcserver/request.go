package rpcserver

import "encoding/json"

// Request is the incoming request envelope.
//
// The Abort field is a WS-only mechanism for cancelling a previously sent
// request. Its value is the nonce of the request to cancel. It can appear:
//   - Alone (abort-only):        {"abort": "n1"}
//   - Merged with a new request: {"abort": "n1", "method": "things.search", "nonce": "n2", ...}
//
// This merged form is useful for debounced operations: a new search query can
// atomically cancel the previous one in a single message, avoiding a round-trip.
// Abort is fire-and-forget — it produces no response. The cancelled request's
// goroutine detects context cancellation and exits silently.
type Request struct {
	ID        string          `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Nonce     string          `json:"nonce,omitempty"`
	Abort     string          `json:"abort,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// Locale is the caller's UI language tag. It rides the envelope rather
	// than a header because the WebSocket transport has no headers to carry it
	// — and the browser's WebSocket constructor cannot set any — while every
	// call in a session goes over that socket.
	Locale string `json:"locale,omitempty"`
}

// Response is the outgoing response envelope. Type is "response" over
// WebSocket, where pushes share the socket, and omitted over HTTP.
//
// ErrorDetails carries optional structured information about an error —
// e.g. the per-field failure list produced by argument validation. It is
// omitted unless the handler returned an APIError with Details populated.
type Response struct {
	Type         string `json:"type,omitempty"`
	ID           string `json:"id,omitempty"`
	Success      bool   `json:"success"`
	Data         any    `json:"data,omitempty"`
	Error        string `json:"error,omitempty"`
	ErrorDetails any    `json:"error_details,omitempty"`
}
