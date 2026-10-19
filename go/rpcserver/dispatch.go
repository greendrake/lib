// Package rpcserver is the server end of the `@greendrake/rpc` wire format:
// a method dispatch table with per-method authentication, served over HTTP
// (one POST endpoint) and over WebSocket (the same envelope plus a correlation
// id and server push). The format — request and response envelopes, error
// codes, the WebSocket framing — is defined in the `@greendrake/rpc` README;
// it is deliberately not JSON-RPC. Argument parsing is rpcserver/rpcargs.
package rpcserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
)

// Dispatcher is the central dispatch engine shared by HTTP and WS transports.
type Dispatcher struct {
	methods   map[string]MethodDef
	nonces    NonceStore
	auth      AuthResolver
	locale    LocaleResolver
	gate      AuthGate
	validator Validator
	clientIP  func(*http.Request) string
	logger    *slog.Logger
}

// LocaleResolver maps the tag a client asked for onto one the application
// actually ships. The policy — which locales exist, how a near miss resolves —
// belongs to the application; this package only carries the value.
type LocaleResolver func(requested string) string

// DispatcherOption configures the Dispatcher.
type DispatcherOption func(*Dispatcher)

// WithNonceStore sets the nonce deduplication store.
func WithNonceStore(ns NonceStore) DispatcherOption {
	return func(d *Dispatcher) { d.nonces = ns }
}

// WithAuthResolver sets the authentication resolver.
func WithAuthResolver(ar AuthResolver) DispatcherOption {
	return func(d *Dispatcher) { d.auth = ar }
}

// WithLocaleResolver sets the resolver for the request envelope's locale.
func WithLocaleResolver(lr LocaleResolver) DispatcherOption {
	return func(d *Dispatcher) { d.locale = lr }
}

// WithAuthGate installs the gate every principal passes before the handler of
// an AuthRequired method runs.
func WithAuthGate(g AuthGate) DispatcherOption {
	return func(d *Dispatcher) { d.gate = g }
}

// WithValidator is the validator every request's handlers parse their
// arguments with (ValidatorFrom).
func WithValidator(v Validator) DispatcherOption {
	return func(d *Dispatcher) { d.validator = v }
}

// WithClientIP derives a request's client address, for both transports. The
// default is the socket's peer (PeerIP); a host behind a proxy it trusts
// derives it from what the proxy forwards.
func WithClientIP(fn func(*http.Request) string) DispatcherOption {
	return func(d *Dispatcher) { d.clientIP = fn }
}

// WithLogger is where the dispatcher and its transports log faults; the
// default is slog.Default().
func WithLogger(l *slog.Logger) DispatcherOption {
	return func(d *Dispatcher) { d.logger = l }
}

// NewDispatcher creates a Dispatcher with the given method table and options.
// A method with an Access that requires no principal, or that no gate is
// installed to enforce, is a mistake in the program, and panics.
func NewDispatcher(methods map[string]MethodDef, opts ...DispatcherOption) *Dispatcher {
	d := &Dispatcher{methods: methods, clientIP: PeerIP, logger: slog.Default()}
	for _, opt := range opts {
		opt(d)
	}
	for name, def := range methods {
		switch {
		case def.Access == nil:
		case def.Auth != AuthRequired:
			panic(fmt.Sprintf("rpcserver: %s has an Access but requires no principal", name))
		case d.gate == nil:
			panic(fmt.Sprintf("rpcserver: %s has an Access and no AuthGate enforces it", name))
		}
	}
	return d
}

// MethodDef returns the dispatch entry for the named method (and whether
// it exists). Transports use this to consult per-method flags
// (e.g. AllowPlain) before invoking Dispatch.
func (d *Dispatcher) MethodDef(name string) (MethodDef, bool) {
	def, ok := d.methods[name]
	return def, ok
}

// Dispatch executes a request. authInfo is pre-resolved by the transport layer.
// Returns a Response (without the Type field set — the transport sets that).
func (d *Dispatcher) Dispatch(hc *HandlerContext, authInfo *AuthInfo, req *Request) *Response {
	// The envelope's locale, resolved by the application, and the validator:
	// set before anything dispatches, so every handler reads the same whichever
	// transport carried the request.
	if d.locale != nil {
		hc.Ctx = WithLocale(hc.Ctx, d.locale(req.Locale))
	}
	if d.validator != nil {
		hc.Ctx = ContextWithValidator(hc.Ctx, d.validator)
	}

	def, ok := d.methods[req.Method]
	if !ok {
		return failure(req, CodeUnknownMethod)
	}

	if def.Auth == AuthRequired && (authInfo == nil || authInfo.UserID == nil) {
		return failure(req, CodeAuthRequired)
	}
	if authInfo != nil {
		hc.Ctx = WithAuth(hc.Ctx, authInfo)
	}
	if def.Auth == AuthRequired && d.gate != nil {
		ctx, err := d.gate(hc.Ctx, authInfo, &def)
		if err != nil {
			return d.errorResponse(hc, req, "auth gate", err)
		}
		hc.Ctx = ctx
	}

	// A nonce is spent only by a request that is going to run: a call refused
	// above can be retried with it.
	if req.Nonce != "" && d.nonces != nil {
		isNew, err := d.nonces.CheckAndSet(hc.Ctx, req.Nonce)
		if err != nil {
			d.logger.Error("rpcserver: nonce check failed", "method", req.Method, "err", err)
		} else if !isNew {
			return failure(req, CodeDuplicateRequest)
		}
	}

	result, err := def.Handler(hc, req.Arguments)
	if err != nil {
		return d.errorResponse(hc, req, "handler", err)
	}

	return &Response{ID: req.ID, Success: true, Data: result}
}

// failure is req's answer with code.
func failure(req *Request, code string) *Response {
	return &Response{ID: req.ID, Success: false, Error: code}
}

// errorResponse answers a failed step of a request: an *APIError by its
// code, anything else as CodeInternal, logged under step.
func (d *Dispatcher) errorResponse(hc *HandlerContext, req *Request, step string, err error) *Response {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return &Response{ID: req.ID, Success: false, Error: apiErr.Code, ErrorDetails: apiErr.Details}
	}
	// Context cancellation is expected when a WS client aborts an in-flight
	// request or disconnects. It is not an application error worth logging.
	if hc.Ctx.Err() == nil {
		d.logger.Error("rpcserver: "+step+" failed", "method", req.Method, "err", err)
	}
	return failure(req, CodeInternal)
}

// marshal is resp on the wire. A result that does not marshal is the
// handler's fault, answered as one.
func (d *Dispatcher) marshal(resp *Response) []byte {
	msg, err := json.Marshal(resp)
	if err == nil {
		return msg
	}
	d.logger.Error("rpcserver: marshal response", "err", err)
	msg, _ = json.Marshal(&Response{Type: resp.Type, ID: resp.ID, Success: false, Error: CodeInternal})
	return msg
}

// principal is the principal token names: nil on a dispatcher without a
// resolver, and an anonymous one for no token or one that does not resolve.
func (d *Dispatcher) principal(r *http.Request, token string) *AuthInfo {
	if d.auth == nil {
		return nil
	}
	if token == "" {
		return &AuthInfo{}
	}
	authInfo, err := d.auth.ResolveToken(r.Context(), token)
	if err != nil {
		return &AuthInfo{}
	}
	return authInfo
}

// PeerIP is the address of a request's socket peer: a request's client
// address unless the dispatcher is told otherwise (WithClientIP).
func PeerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
