package rpcserver

import (
	"context"
	"encoding/json"
)

// AuthRequirement is whether a method needs a principal.
type AuthRequirement int

const (
	AuthNone     AuthRequirement = iota // anyone may call it
	AuthRequired                        // only a principal, whom the AuthGate admits
)

// HandlerContext carries transport-agnostic information to method handlers.
type HandlerContext struct {
	Ctx context.Context
	// ClientIP is the caller's address, as the dispatcher's WithClientIP
	// derives it.
	ClientIP string
}

// HandlerFunc is the signature for method implementations.
type HandlerFunc func(hc *HandlerContext, args json.RawMessage) (any, error)

// MethodDef describes a single API method's handler and what it asks of its
// caller.
//
// Access is what the method asks of its principal beyond there being one, in
// the terms of the host's AuthGate, which alone reads it. A method with an
// Access requires a principal, and the dispatcher serving it has a gate:
// NewDispatcher refuses a table breaking either.
//
// AllowPlain opts a method out of the transport's
// Encoding.RequireEncodedRequest: when the HTTP transport sees a plain-JSON
// body (detected structurally via isLikelyJSON) it accepts it only if the
// dispatched method's AllowPlain is true; otherwise it is rejected as a
// generic malformed request. Reserved for early-boot endpoints (e.g. an
// app's init) whose caller — an inline <head> script, say — cannot reach the
// client's encoder. (The response still honours EncodeResponse — AllowPlain
// governs the request direction only.)
type MethodDef struct {
	Handler    HandlerFunc
	Auth       AuthRequirement
	Access     any
	AllowPlain bool
}
