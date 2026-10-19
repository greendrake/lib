package rpcservertest_test

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"greendrake.nz/go/rpcserver"
	"greendrake.nz/go/rpcserver/rpcargs"
	"greendrake.nz/go/rpcserver/rpcservertest"
)

type room struct {
	Lights map[string]bool `json:"lights"`
	Name   string          `json:"name"`
}

type house struct {
	Rooms map[string]room `json:"rooms"`
}

// A kept document follows its patches — members replaced, objects merged, nulls removing
// — and a call's patch is merged before its answer is returned.
func TestKeptFollowsPatchesAndCalls(t *testing.T) {
	var mu sync.Mutex
	state := house{Rooms: map[string]room{
		"hall":    {Lights: map[string]bool{"ceiling": true, "lamp": false}, Name: "Hall"},
		"kitchen": {Lights: map[string]bool{"ceiling": false}, Name: "Kitchen"},
	}}
	b := rpcserver.NewBroadcast()
	doc := b.Document("house", func() any {
		mu.Lock()
		defer mu.Unlock()
		return state
	})
	change := func(edit func()) {
		mu.Lock()
		edit()
		mu.Unlock()
		doc.Refresh()
	}
	server := httptest.NewServer(b.Handler(rpcserver.NewDispatcher(map[string]rpcserver.MethodDef{
		"room.remove": {Auth: rpcserver.AuthNone, Handler: func(_ *rpcserver.HandlerContext, raw json.RawMessage) (any, error) {
			var in struct {
				Room string `json:"room"`
			}
			if err := rpcargs.ParseArguments(raw, &in); err != nil {
				return nil, err
			}
			mu.Lock()
			_, held := state.Rooms[in.Room]
			mu.Unlock()
			if !held {
				return nil, rpcserver.NewAPIError("ROOM_NOT_FOUND")
			}
			change(func() { delete(state.Rooms, in.Room) })
			return true, nil
		}},
	})))
	defer server.Close()

	kept := rpcservertest.Keep[house](t, rpcservertest.Dial(t, server.URL), "house")
	if !reflect.DeepEqual(kept.Doc, state) {
		t.Fatalf("greeted with %+v", kept.Doc)
	}

	change(func() {
		state.Rooms["hall"] = room{Lights: map[string]bool{"ceiling": true}, Name: "Hallway"}
	})
	kept.Next(t)
	if want := (room{Lights: map[string]bool{"ceiling": true}, Name: "Hallway"}); !reflect.DeepEqual(kept.Doc.Rooms["hall"], want) {
		t.Fatalf("hall %+v", kept.Doc.Rooms["hall"])
	}

	if data, code := kept.Call(t, "room.remove", map[string]string{"room": "kitchen"}); code != "" || string(data) != "true" {
		t.Fatalf("room.remove answered %s, %q", data, code)
	}
	if _, held := kept.Doc.Rooms["kitchen"]; held {
		t.Fatalf("the kitchen is kept after its removal was answered: %+v", kept.Doc)
	}
	if _, code := kept.Call(t, "room.remove", map[string]string{"room": "kitchen"}); code != "ROOM_NOT_FOUND" {
		t.Fatalf("a second removal answered %q", code)
	}

	change(func() { state.Rooms["attic"] = room{Lights: map[string]bool{}, Name: "Attic"} })
	change(func() { state.Rooms["attic"] = room{Lights: map[string]bool{"skylight": true}, Name: "Attic"} })
	kept.Until(t, func(h house) bool { return h.Rooms["attic"].Lights["skylight"] })
}
