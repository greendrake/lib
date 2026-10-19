package rpcserver

import (
	"context"
	"sync"
)

// ConnRegistry maps user IDs to their active WS connections.
// A user may have multiple connections (multiple tabs/devices).
type ConnRegistry struct {
	mu    sync.RWMutex
	conns map[any]map[*WSConn]struct{}
}

var _ Emitter = (*ConnRegistry)(nil)

func NewConnRegistry() *ConnRegistry {
	return &ConnRegistry{
		conns: make(map[any]map[*WSConn]struct{}),
	}
}

func (r *ConnRegistry) Register(userID any, ws *WSConn) {
	if userID == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conns[userID] == nil {
		r.conns[userID] = make(map[*WSConn]struct{})
	}
	r.conns[userID][ws] = struct{}{}
}

func (r *ConnRegistry) Unregister(ws *WSConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	auth := ws.getAuth()
	if auth == nil || auth.UserID == nil {
		return
	}
	userID := auth.UserID
	if conns, ok := r.conns[userID]; ok {
		delete(conns, ws)
		if len(conns) == 0 {
			delete(r.conns, userID)
		}
	}
}

// Emit pushes event, carrying data, to every live connection of userID.
func (r *ConnRegistry) Emit(_ context.Context, userID any, event string, data any) {
	msg := mustMarshal(eventFrame{Type: "event", Event: event, Data: data})
	r.mu.RLock()
	defer r.mu.RUnlock()
	for ws := range r.conns[userID] {
		ws.Send(msg)
	}
}
