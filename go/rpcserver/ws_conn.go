package rpcserver

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
)

// WSConn wraps a single WebSocket connection.
type WSConn struct {
	conn     *websocket.Conn
	send     chan []byte
	authInfo *AuthInfo
	mu       sync.RWMutex
	logger   *slog.Logger

	// cancel ends the connection's context, which ends its read and write loops and so
	// tears it down; it is how Send gives up on a peer that has stopped reading, and
	// overflowed records that it did.
	cancel     context.CancelFunc
	overflow   sync.Once
	overflowed atomic.Bool

	// In-flight request cancellation support.
	// When the read loop dispatches a request with a non-empty nonce, it derives
	// a per-request context (via context.WithCancel) and stores the CancelFunc
	// here keyed by that nonce. A subsequent message with an "abort" field
	// referencing the same nonce can then look up and call the CancelFunc to
	// propagate cancellation all the way down to the database/search drivers,
	// which respect context cancellation and will abandon the operation early.
	inflightMu sync.Mutex
	inflight   map[string]context.CancelFunc

	// wg tracks goroutines spawned for concurrent request dispatch. ServeHTTP
	// waits on it before closing the send channel to prevent send-on-closed-channel
	// panics when the connection drops while requests are still in flight.
	wg sync.WaitGroup
}

func newWSConn(conn *websocket.Conn, cancel context.CancelFunc, logger *slog.Logger) *WSConn {
	return &WSConn{
		conn:     conn,
		send:     make(chan []byte, 64),
		inflight: make(map[string]context.CancelFunc),
		cancel:   cancel,
		logger:   logger,
	}
}

func (ws *WSConn) setAuth(auth *AuthInfo) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.authInfo = auth
}

func (ws *WSConn) getAuth() *AuthInfo {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.authInfo
}

// Send enqueues a message for delivery, never blocking: it runs under the locks of
// whatever is fanning out. A full buffer is a peer that has stopped keeping up, and a
// message dropped would leave it silently wrong — what a client keeps is the sum of
// what it was sent — so the socket is cancelled instead, once, and its own goroutines
// tear it down. The client's reconnect then starts it afresh.
func (ws *WSConn) Send(msg []byte) {
	select {
	case ws.send <- msg:
	default:
		ws.overflow.Do(func() {
			ws.logger.Warn("rpcserver: WebSocket send buffer full; closing the socket")
			ws.overflowed.Store(true)
			ws.cancel()
		})
	}
}

// trackRequest registers a per-request CancelFunc keyed by nonce so it can be
// invoked later by an abort field on a subsequent message.
func (ws *WSConn) trackRequest(nonce string, cancel context.CancelFunc) {
	ws.inflightMu.Lock()
	defer ws.inflightMu.Unlock()
	ws.inflight[nonce] = cancel
}

// untrackRequest removes a completed request from the in-flight map.
// Safe to call even if the entry was already removed by cancelRequest.
func (ws *WSConn) untrackRequest(nonce string) {
	ws.inflightMu.Lock()
	defer ws.inflightMu.Unlock()
	delete(ws.inflight, nonce)
}

// cancelRequest cancels an in-flight request by invoking its CancelFunc.
// Returns true if the request was found and cancelled, false if it was already
// completed or never existed. Abort is idempotent by design.
func (ws *WSConn) cancelRequest(nonce string) bool {
	ws.inflightMu.Lock()
	defer ws.inflightMu.Unlock()
	cancel, ok := ws.inflight[nonce]
	if ok {
		cancel()
		delete(ws.inflight, nonce)
	}
	return ok
}
