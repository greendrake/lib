package rpcserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// The WebSocket methods the transport answers itself, and the query parameter a
// socket can be opened with a token in.
const (
	MethodAuthToken  = "auth.token"
	MethodAuthLogout = "auth.logout"
	TokenParam       = "token"
)

// WSOption configures NewWSHandler.
type WSOption func(*wsHandler)

// WithWSEncoding puts an Encoding on the WS handler.
func WithWSEncoding(e Encoding) WSOption {
	e = e.mustHaveCodec()
	return func(h *wsHandler) { h.encoding = e }
}

// WithWSOnOpen registers a callback run once per connection, once it is ready to be
// written to. An app that broadcasts to its own set of live sockets uses this to add
// the connection and greet it with current state.
func WithWSOnOpen(fn func(*WSConn)) WSOption {
	return func(h *wsHandler) { h.onOpen = fn }
}

// WithWSOnClose registers a callback run once per connection, after its last message
// has been handled and before it is torn down. It is the counterpart of WithWSOnOpen.
func WithWSOnClose(fn func(*WSConn)) WSOption {
	return func(h *wsHandler) { h.onClose = fn }
}

// WithWSReadLimit is the largest message a client may send, in bytes: a larger one
// closes its socket. Unset, it is the WebSocket library's 32 KiB.
func WithWSReadLimit(n int64) WSOption {
	return func(h *wsHandler) { h.readLimit = n }
}

// WithWSOriginPatterns admits browser sockets from the origins patterns match,
// beside the endpoint's own, which is always admitted; a socket from any other
// origin is refused at the handshake. A pattern holding a scheme
// ("https://app.example.com", "app://localhost") is matched against the
// origin's scheme and host, any other against its host alone, both with
// path.Match. A client sending no Origin — not a browser — is not checked.
func WithWSOriginPatterns(patterns ...string) WSOption {
	return func(h *wsHandler) { h.originPatterns = patterns }
}

// NewWSHandler returns an http.Handler that upgrades to WebSocket.
func NewWSHandler(d *Dispatcher, registry *ConnRegistry, opts ...WSOption) http.Handler {
	h := &wsHandler{dispatcher: d, registry: registry}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type wsHandler struct {
	dispatcher     *Dispatcher
	registry       *ConnRegistry
	encoding       Encoding
	onOpen         func(*WSConn)
	onClose        func(*WSConn)
	readLimit      int64
	originPatterns []string
}

func (h *wsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.originPatterns})
	if err != nil {
		// Accept has answered the client; a refused origin is worth an
		// operator's notice, the more so when the list is what is wrong.
		h.dispatcher.logger.Warn("rpcserver: WebSocket handshake refused", "origin", r.Header.Get("Origin"), "err", err)
		return
	}

	if h.readLimit > 0 {
		conn.SetReadLimit(h.readLimit)
	}

	// The socket's own context: it ends when the client goes, or when Send gives up on
	// a client that has stopped reading.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	ws := newWSConn(conn, cancel, h.dispatcher.logger)

	if token := r.URL.Query().Get(TokenParam); token != "" {
		if authInfo := h.dispatcher.principal(r, token); authInfo != nil && authInfo.UserID != nil {
			ws.setAuth(authInfo)
			h.registry.Register(authInfo.UserID, ws)
		}
	}

	go h.writeLoop(ctx, ws)

	// Run the open hook once the write loop is draining, so anything it sends
	// goes out immediately rather than sitting in the send buffer.
	if h.onOpen != nil {
		h.onOpen(ws)
	}

	h.readLoop(ctx, ws, h.dispatcher.clientIP(r))

	// Wait for all in-flight request goroutines to finish before closing the
	// send channel. Without this, a goroutine that completes after readLoop
	// exits would panic writing to a closed channel.
	ws.wg.Wait()

	if h.onClose != nil {
		h.onClose(ws)
	}

	h.registry.Unregister(ws)
	close(ws.send)
}

func (h *wsHandler) readLoop(ctx context.Context, ws *WSConn, clientIP string) {
	defer func() {
		if ws.overflowed.Load() {
			// The client has stopped reading: a close handshake would only wait it out.
			ws.conn.CloseNow()
			return
		}
		ws.conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		_, msgBytes, err := ws.conn.Read(ctx)
		if err != nil {
			// The client went, or broke the protocol: either way the socket
			// is done, and there is no one left to answer.
			return
		}

		// Auto-detect the message form (plain vs encoded), the same way the
		// HTTP path does.
		ready, plain, err := h.encoding.decodeRequest(msgBytes)

		// Strict mode (RequireEncodedRequest): WS messages must be encoded. The
		// client (loaded before the socket opens) always encodes, so a plain
		// frame is a protocol violation — rejected with the same generic error
		// as a malformed frame, never signalling the encoding requirement. A
		// PlainTransport principal (a trusted non-browser caller, e.g. a static
		// admin key) is exempt: it runs neither encoder nor decoder. There is no
		// per-method AllowPlain over WS: no early-boot path predates the client
		// here.
		if plain && h.encoding.RequireEncodedRequest {
			if auth := ws.getAuth(); auth == nil || !auth.PlainTransport {
				h.sendWSResponse(ws, &Response{Type: "response", Success: false, Error: CodeInvalidJSON})
				continue
			}
		}

		var req Request
		if err == nil {
			err = json.Unmarshal(ready, &req)
		}
		if err != nil {
			h.sendWSResponse(ws, &Response{Type: "response", Success: false, Error: CodeInvalidJSON})
			continue
		}

		// Process abort before anything else. The abort field references the
		// nonce of a previously sent request whose context should be cancelled.
		// This is fire-and-forget: no response is produced for the abort itself.
		// It can appear alone (abort-only, no method) or merged with a new
		// method call, e.g. a debounced search that cancels the previous query
		// and starts a new one in a single message.
		if req.Abort != "" {
			ws.cancelRequest(req.Abort)
		}

		// Abort-only message (no method to dispatch).
		if req.Method == "" {
			continue
		}

		// Protocol-level commands are handled synchronously in the read loop.
		// They are lightweight, non-blocking operations that manage connection
		// state and must complete before subsequent messages are processed to
		// maintain correct ordering guarantees.
		switch req.Method {
		case MethodAuthToken:
			h.handleAuthToken(ctx, ws, &req)
			continue
		case MethodAuthLogout:
			h.handleAuthLogout(ws, &req)
			continue
		}

		// API methods are dispatched concurrently in their own goroutines.
		// This keeps the read loop responsive so that:
		//  1. New requests can be received while earlier ones are still executing.
		//  2. Abort fields on incoming messages are processed immediately to
		//     cancel in-flight work.
		//
		// Each goroutine gets a derived context (via WithCancel) so that
		// cancellation propagates through the dispatcher all the way to the
		// database/search drivers, which respect context cancellation and will
		// abandon in-progress I/O.
		reqCtx, cancel := context.WithCancel(ctx)

		// Only requests with a nonce are abortable — the nonce is how a
		// subsequent message references this request in its abort field.
		if req.Nonce != "" {
			ws.trackRequest(req.Nonce, cancel)
		}

		ws.wg.Add(1)
		go func(req Request) {
			defer ws.wg.Done()
			defer cancel()
			if req.Nonce != "" {
				defer ws.untrackRequest(req.Nonce)
			}

			hc := &HandlerContext{Ctx: reqCtx, ClientIP: clientIP}
			resp := h.dispatcher.Dispatch(hc, ws.getAuth(), &req)

			// If the per-request context was cancelled (by an abort or
			// connection close), silently discard the response. The client
			// initiated the abort and already discarded interest in this
			// request — sending a response would be wasteful noise.
			if reqCtx.Err() != nil {
				return
			}

			resp.Type = "response"
			h.sendWSResponse(ws, resp)
		}(req)
	}
}

// handleAuthToken binds the socket to the principal its one argument, a token,
// names.
func (h *wsHandler) handleAuthToken(ctx context.Context, ws *WSConn, req *Request) {
	if h.dispatcher.auth == nil {
		h.sendWSResponse(ws, &Response{Type: "response", ID: req.ID, Success: false, Error: CodeAuthNotConfigured})
		return
	}

	var arguments []string
	if err := json.Unmarshal(req.Arguments, &arguments); err != nil || len(arguments) != 1 {
		h.sendWSResponse(ws, &Response{Type: "response", ID: req.ID, Success: false, Error: CodeInvalidArgument})
		return
	}

	authInfo, err := h.dispatcher.auth.ResolveToken(ctx, arguments[0])
	if err != nil || authInfo.UserID == nil {
		h.sendWSResponse(ws, &Response{Type: "response", ID: req.ID, Success: false, Error: CodeUnauthenticated})
		return
	}

	// Unregister old identity, set new, re-register
	h.registry.Unregister(ws)
	ws.setAuth(authInfo)
	h.registry.Register(authInfo.UserID, ws)

	h.sendWSResponse(ws, &Response{Type: "response", ID: req.ID, Success: true})
}

func (h *wsHandler) handleAuthLogout(ws *WSConn, req *Request) {
	h.registry.Unregister(ws)
	ws.setAuth(nil)
	h.sendWSResponse(ws, &Response{Type: "response", ID: req.ID, Success: true})
}

func (h *wsHandler) writeLoop(ctx context.Context, ws *WSConn) {
	for msg := range ws.send {
		// A PlainTransport principal (e.g. a static admin key) opts out of
		// response encoding for the whole connection.
		if auth := ws.getAuth(); h.encoding.EncodeResponse && (auth == nil || !auth.PlainTransport) {
			msg = h.encoding.Codec.Encode(msg)
		}
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := ws.conn.Write(writeCtx, websocket.MessageText, msg)
		cancel()
		if err != nil {
			// The socket is gone; the read loop ends it.
			return
		}
	}
}

func (h *wsHandler) sendWSResponse(ws *WSConn, resp *Response) {
	ws.Send(h.dispatcher.marshal(resp))
}
