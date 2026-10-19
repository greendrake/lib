package rpcserver

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"greendrake.nz/go/rpcserver/rpcservertest"
)

const broadcastTimeout = 2 * time.Second

// noMethods is a dispatcher with nothing to call.
func noMethods() *Dispatcher { return NewDispatcher(map[string]MethodDef{}) }

func (b *Broadcast) connCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.conns)
}

// awaitConnCount polls until the connection set reaches n, which is how a test waits
// for an open or close hook to have run.
func awaitConnCount(t *testing.T, b *Broadcast, n int) {
	t.Helper()
	deadline := time.Now().Add(broadcastTimeout)
	for time.Now().Before(deadline) {
		if b.connCount() == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("connection count: got %d, want %d", b.connCount(), n)
}

func TestBroadcastHandlerRefusesOpenAndCloseHooks(t *testing.T) {
	for name, opt := range map[string]WSOption{
		"open":  WithWSOnOpen(func(*WSConn) {}),
		"close": WithWSOnClose(func(*WSConn) {}),
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("a Broadcast took a hook of its own sockets")
				}
			}()
			NewBroadcast().Handler(noMethods(), opt)
		})
	}
}

func TestBroadcastPushReachesEveryConnection(t *testing.T) {
	b := NewBroadcast()
	server := httptest.NewServer(b.Handler(noMethods()))
	defer server.Close()

	first := rpcservertest.Dial(t, server.URL)
	second := rpcservertest.Dial(t, server.URL)
	awaitConnCount(t, b, 2)

	b.Push("power", map[string]any{"load": 798})

	for _, conn := range []*websocket.Conn{first, second} {
		if data := rpcservertest.Data(t, conn, "power"); data["load"] != float64(798) {
			t.Fatalf("push: got %v", data)
		}
	}
}

func TestBroadcastDropsClosedConnections(t *testing.T) {
	b := NewBroadcast()
	server := httptest.NewServer(b.Handler(noMethods()))
	defer server.Close()

	going := rpcservertest.Dial(t, server.URL)
	staying := rpcservertest.Dial(t, server.URL)
	awaitConnCount(t, b, 2)

	going.Close(websocket.StatusNormalClosure, "")
	awaitConnCount(t, b, 1)

	b.Push("power", map[string]any{"mode": 4})
	if data := rpcservertest.Data(t, staying, "power"); data["mode"] != float64(4) {
		t.Fatalf("the remaining connection missed the push: got %v", data)
	}
}

func TestBroadcastDispatchesMethods(t *testing.T) {
	b := NewBroadcast()
	methods := map[string]MethodDef{
		"thing.restart": {
			Auth: AuthNone,
			Handler: func(*HandlerContext, json.RawMessage) (any, error) {
				return true, nil
			},
		},
	}
	server := httptest.NewServer(b.Handler(NewDispatcher(methods)))
	defer server.Close()

	conn := rpcservertest.Dial(t, server.URL)
	if resp := wsRequest(t, conn, &Request{ID: "r1", Method: "thing.restart"}); !resp.Success || resp.ID != "r1" || resp.Data != true {
		t.Fatalf("response: %+v", resp)
	}
}
