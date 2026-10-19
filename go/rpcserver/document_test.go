package rpcserver

import (
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/coder/websocket"

	"greendrake.nz/go/rpcserver/rpcservertest"
)

// state is a producer's state for a test, changed under its own lock.
type state struct {
	mu     sync.Mutex
	values map[string]any
}

func (s *state) set(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
}

func (s *state) remove(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
}

func (s *state) produce() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := make(map[string]any, len(s.values))
	for key, value := range s.values {
		clone[key] = value
	}
	return clone
}

// serve serves the Broadcast for the test, and dials a socket on it.
func serve(t *testing.T, b *Broadcast) (*httptest.Server, *websocket.Conn) {
	t.Helper()
	server := httptest.NewServer(b.Handler(noMethods()))
	t.Cleanup(server.Close)
	return server, rpcservertest.Dial(t, server.URL)
}

func TestDocumentGreetsAnOpeningSocket(t *testing.T) {
	b := NewBroadcast()
	b.Document("power", func() any { return map[string]any{"mode": 3} })
	b.Document("weather", func() any { return map[string]any{"temperature": 18} })
	_, conn := serve(t, b)

	// Greeted with each, in the order they were made.
	if doc, _ := rpcservertest.State(t, conn, "power"); !reflect.DeepEqual(doc, map[string]any{"mode": 3.0}) {
		t.Fatalf("first document: %v", doc)
	}
	if doc, _ := rpcservertest.State(t, conn, "weather"); !reflect.DeepEqual(doc, map[string]any{"temperature": 18.0}) {
		t.Fatalf("second document: %v", doc)
	}
}

func TestDocumentPatchesWhatChanged(t *testing.T) {
	b := NewBroadcast()
	s := &state{values: map[string]any{"load": 798, "mode": 3}}
	doc := b.Document("power", s.produce)
	_, conn := serve(t, b)
	rpcservertest.State(t, conn, "power")

	s.set("load", 801)
	doc.Refresh()
	if _, patch := rpcservertest.State(t, conn, "power"); !reflect.DeepEqual(patch, map[string]any{"load": 801.0}) {
		t.Fatalf("patch of a change: %v", patch)
	}

	// Nothing changed, so nothing goes: the next frame is the removal after it.
	doc.Refresh()
	s.remove("mode")
	doc.Refresh()
	if _, patch := rpcservertest.State(t, conn, "power"); !reflect.DeepEqual(patch, map[string]any{"mode": nil}) {
		t.Fatalf("patch of a removal: %v", patch)
	}
}

func TestDocumentDropsNullMembers(t *testing.T) {
	b := NewBroadcast()
	s := &state{values: map[string]any{"gate": map[string]any{"State": "online", "Streams": nil}, "porch": nil}}
	doc := b.Document("cameras", s.produce)
	_, conn := serve(t, b)

	if greeted, _ := rpcservertest.State(t, conn, "cameras"); !reflect.DeepEqual(greeted, map[string]any{"gate": map[string]any{"State": "online"}}) {
		t.Fatalf("document: %v", greeted)
	}

	// A member becoming null is a removal.
	s.set("gate", nil)
	s.set("porch", map[string]any{"State": "offline"})
	doc.Refresh()
	if _, patch := rpcservertest.State(t, conn, "cameras"); !reflect.DeepEqual(patch, map[string]any{"gate": nil, "porch": map[string]any{"State": "offline"}}) {
		t.Fatalf("patch: %v", patch)
	}
}

func TestDocumentMadeAfterASocketOpenedGreetsIt(t *testing.T) {
	b := NewBroadcast()
	_, conn := serve(t, b)
	awaitConnCount(t, b, 1)

	b.Document("power", func() any { return map[string]any{"mode": 3} })
	if doc, _ := rpcservertest.State(t, conn, "power"); !reflect.DeepEqual(doc, map[string]any{"mode": 3.0}) {
		t.Fatalf("document: %v", doc)
	}
}

func (d *Document) connCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.conns)
}

func TestDocumentForgetsClosedSockets(t *testing.T) {
	b := NewBroadcast()
	doc := b.Document("power", func() any { return map[string]any{"mode": 3} })
	_, conn := serve(t, b)
	rpcservertest.State(t, conn, "power")
	if got := doc.connCount(); got != 1 {
		t.Fatalf("sockets keeping the document: got %d, want 1", got)
	}

	conn.Close(websocket.StatusNormalClosure, "")
	awaitConnCount(t, b, 0)
	if got := doc.connCount(); got != 0 {
		t.Fatalf("sockets keeping the document after the close: got %d, want 0", got)
	}
}

// Sockets open while the state changes as fast as it can be published. Whenever a
// socket opens, its document is the first frame it gets, and each patch after it is
// newer than everything before it: no patch overtakes the greeting, and none computed
// before it lands after it.
func TestDocumentGreetingIsNeverOvertaken(t *testing.T) {
	b := NewBroadcast()
	var n atomic.Int64
	doc := b.Document("counter", func() any { return map[string]any{"n": n.Load()} })
	server := httptest.NewServer(b.Handler(noMethods()))
	defer server.Close()

	// Fewer changes per socket than its send buffer holds, so none is closed for
	// falling behind while its test reads.
	const burst = 30
	for range 20 {
		changed := make(chan struct{})
		go func() {
			defer close(changed)
			for range burst {
				n.Add(1)
				doc.Refresh()
			}
		}()
		conn := rpcservertest.Dial(t, server.URL)
		greeted, _ := rpcservertest.State(t, conn, "counter")
		if greeted == nil {
			t.Fatal("the first frame is a patch")
		}
		last := greeted["n"].(float64)
		<-changed
		for final := float64(n.Load()); last < final; {
			_, patch := rpcservertest.State(t, conn, "counter")
			if patch == nil {
				t.Fatal("a second document")
			}
			next := patch["n"].(float64)
			if next <= last {
				t.Fatalf("patch %v after %v", next, last)
			}
			last = next
		}
		conn.Close(websocket.StatusNormalClosure, "")
	}
}

func TestDocumentRefusesAStateThatIsNoObject(t *testing.T) {
	for name, produce := range map[string]func() any{
		"a number": func() any { return 3 },
		"a nil map": func() any {
			var m map[string]any
			return m
		},
		"unmarshalable": func() any { return map[string]any{"f": func() {}} },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("a document was made of it")
				}
			}()
			NewBroadcast().Document("bad", produce)
		})
	}
}
