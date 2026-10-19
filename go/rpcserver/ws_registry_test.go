package rpcserver

import (
	"context"
	"testing"
)

func TestConnRegistry_RegisterUnregister(t *testing.T) {
	r := NewConnRegistry()
	ws := &WSConn{send: make(chan []byte, 64)}
	ws.setAuth(&AuthInfo{UserID: "u1"})

	r.Register("u1", ws)
	r.Register(nil, ws)

	r.mu.RLock()
	conns := len(r.conns["u1"])
	r.mu.RUnlock()
	if conns != 1 || len(r.conns) != 1 {
		t.Fatalf("expected 1 connection, got %v", r.conns)
	}

	r.Unregister(ws)

	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.conns) != 0 {
		t.Fatalf("expected no connections after unregister, got %v", r.conns)
	}
}

// An event goes to every connection of its user, and to no one else's.
func TestConnRegistry_Emit(t *testing.T) {
	r := NewConnRegistry()
	conn := func(userID string) *WSConn {
		ws := &WSConn{send: make(chan []byte, 64)}
		ws.setAuth(&AuthInfo{UserID: userID})
		r.Register(userID, ws)
		return ws
	}
	ws1, ws2, other := conn("u1"), conn("u1"), conn("u2")

	r.Emit(context.Background(), "u1", "test.event", map[string]string{"key": "val"})

	expected := `{"type":"event","event":"test.event","data":{"key":"val"}}`
	for _, ws := range []*WSConn{ws1, ws2} {
		if msg := <-ws.send; string(msg) != expected {
			t.Fatalf("expected %s, got %s", expected, msg)
		}
	}
	if len(other.send) != 0 {
		t.Fatal("another user's connection was sent the event")
	}
	r.Emit(context.Background(), "nobody", "test.event", nil)
}

func TestConnRegistry_EmitUnmarshalablePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("data that does not marshal must panic")
		}
	}()
	NewConnRegistry().Emit(context.Background(), "u1", "test.event", func() {})
}

func TestConnRegistry_UnregisterNilAuth(t *testing.T) {
	r := NewConnRegistry()
	ws := &WSConn{send: make(chan []byte, 64)}
	// No auth set — should not panic
	r.Unregister(ws)
}
