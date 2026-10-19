package rpcserver

import (
	"net/http"
	"slices"
	"sync"
)

// Broadcast is the API endpoint of an app without auth: one RPC WebSocket carrying both
// the app's calls and everything it pushes, to every live socket alike. It knows
// nothing of what it carries: it is handed the methods, and its producers publish
// through it — state as a Document, a signal through Push.
type Broadcast struct {
	mu    sync.RWMutex
	conns map[*WSConn]struct{}
	docs  []*Document
}

func NewBroadcast() *Broadcast {
	return &Broadcast{conns: make(map[*WSConn]struct{})}
}

// Handler serves the endpoint over the dispatcher, greeting each socket that opens
// with every Document. opts configure the transport, less the open and close hooks,
// which are the Broadcast's own.
func (b *Broadcast) Handler(d *Dispatcher, opts ...WSOption) http.Handler {
	var probe wsHandler
	for _, opt := range opts {
		opt(&probe)
	}
	if probe.onOpen != nil || probe.onClose != nil {
		panic("rpcserver: a Broadcast owns its sockets' open and close hooks")
	}
	// The registry goes unused, there being no addressee without auth, but the
	// handler unregisters on teardown unconditionally.
	return NewWSHandler(d, NewConnRegistry(), append(slices.Clone(opts),
		WithWSOnOpen(func(ws *WSConn) {
			b.mu.Lock()
			b.conns[ws] = struct{}{}
			docs := slices.Clone(b.docs)
			b.mu.Unlock()
			for _, d := range docs {
				d.greet(ws)
			}
		}),
		WithWSOnClose(func(ws *WSConn) {
			b.mu.Lock()
			delete(b.conns, ws)
			docs := slices.Clone(b.docs)
			b.mu.Unlock()
			for _, d := range docs {
				d.leave(ws)
			}
		}),
	)...)
}

// Push fans a signal out to every live socket as a bare {type, data} frame: word that
// something happened, which the client answers by re-reading. State that the client
// keeps goes in a Document instead.
func (b *Broadcast) Push(dataType string, data any) {
	msg := mustMarshal(dataFrame{Type: dataType, Data: data})
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ws := range b.conns {
		ws.Send(msg)
	}
}
