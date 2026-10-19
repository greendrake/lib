// Package rpcservertest is a client of an rpcserver WebSocket endpoint, for tests: the
// socket a browser opens, and the frames it reads off it.
package rpcservertest

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// timeout bounds each dial and read: a frame that does not come fails the test.
const timeout = 5 * time.Second

// Dial opens a socket on the endpoint at url, an httptest server's http:// one with any
// query, closed when the test ends.
func Dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http"), nil)
	if err != nil {
		t.Fatalf("rpcservertest: dial %s: %v", url, err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

// Read reads the socket's next message into v.
func Read(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("rpcservertest: no message: %v", err)
	}
	if err := json.Unmarshal(msg, v); err != nil {
		t.Fatalf("rpcservertest: %s does not decode as %T: %v", msg, v, err)
	}
}

// Data reads the socket's next frame, which must be a bare data frame of dataType, and
// returns its data.
func Data(t *testing.T, conn *websocket.Conn, dataType string) map[string]any {
	t.Helper()
	var frame struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	Read(t, conn, &frame)
	if frame.Type != dataType {
		t.Fatalf("rpcservertest: a %q frame where a %q one was due", frame.Type, dataType)
	}
	return frame.Data
}

// State reads the socket's next frame, which must be of the document dataType, and
// returns the doc or the patch it carries: exactly one of them is non-nil.
func State(t *testing.T, conn *websocket.Conn, dataType string) (doc, patch map[string]any) {
	t.Helper()
	return split(t, Data(t, conn, dataType))
}

// split is the doc or the patch a document's frame carries as its data.
func split(t *testing.T, data map[string]any) (doc, patch map[string]any) {
	t.Helper()
	doc, _ = data["doc"].(map[string]any)
	patch, _ = data["patch"].(map[string]any)
	if len(data) != 1 || (doc == nil) == (patch == nil) {
		t.Fatalf("rpcservertest: %v is no frame of a document", data)
	}
	return doc, patch
}

// Kept is a socket as a page uses it: one document kept — greeted whole, each patch
// merged in — and decoded as D, and calls made on it.
type Kept[D any] struct {
	conn     *websocket.Conn
	dataType string
	raw      map[string]any
	calls    int
	// Doc is the document as the last frame read left it.
	Doc D
}

// Keep reads the greeting of the document dataType off conn, which must be the socket's
// next frame, and keeps the document from then on.
func Keep[D any](t *testing.T, conn *websocket.Conn, dataType string) *Kept[D] {
	t.Helper()
	doc, patch := State(t, conn, dataType)
	if patch != nil {
		t.Fatalf("rpcservertest: a patch %v where the greeting was due", patch)
	}
	k := &Kept[D]{conn: conn, dataType: dataType, raw: doc}
	k.decode(t)
	return k
}

// Next reads the socket's next frame, which must be a patch of the document, and merges
// it in.
func (k *Kept[D]) Next(t *testing.T) {
	t.Helper()
	k.patch(t, Data(t, k.conn, k.dataType))
}

// Until reads patches until the document is as done wants it.
func (k *Kept[D]) Until(t *testing.T, done func(D) bool) {
	t.Helper()
	for !done(k.Doc) {
		k.Next(t)
	}
}

// Call calls method with args as its named arguments, and reads the socket up to the
// response, merging in the patches ahead of it: what a call changes is published before
// it is answered. It returns the response's data, or its error code.
func (k *Kept[D]) Call(t *testing.T, method string, args any) (data json.RawMessage, code string) {
	t.Helper()
	k.calls++
	id := strconv.Itoa(k.calls)
	arguments, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("rpcservertest: arguments %v: %v", args, err)
	}
	request, err := json.Marshal(map[string]any{"id": id, "method": method, "arguments": json.RawMessage(arguments)})
	if err != nil {
		t.Fatalf("rpcservertest: request %s: %v", method, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := k.conn.Write(ctx, websocket.MessageText, request); err != nil {
		t.Fatalf("rpcservertest: send %s: %v", method, err)
	}
	for {
		var frame struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Data  json.RawMessage `json:"data"`
			Error string          `json:"error"`
		}
		Read(t, k.conn, &frame)
		switch frame.Type {
		case "response":
			if frame.ID != id {
				t.Fatalf("rpcservertest: the response to %q where %q's was due", frame.ID, id)
			}
			return frame.Data, frame.Error
		case k.dataType:
			var state map[string]any
			if err := json.Unmarshal(frame.Data, &state); err != nil {
				t.Fatalf("rpcservertest: %s is no frame of a document: %v", frame.Data, err)
			}
			k.patch(t, state)
		default:
			t.Fatalf("rpcservertest: a %q frame during %s", frame.Type, method)
		}
	}
}

// patch merges in the patch a document frame's data carries.
func (k *Kept[D]) patch(t *testing.T, data map[string]any) {
	t.Helper()
	doc, patch := split(t, data)
	if doc != nil {
		t.Fatalf("rpcservertest: a greeting %v where a patch was due", doc)
	}
	merge(k.raw, patch)
	k.decode(t)
}

func (k *Kept[D]) decode(t *testing.T) {
	t.Helper()
	data, err := json.Marshal(k.raw)
	if err != nil {
		t.Fatalf("rpcservertest: %v: %v", k.raw, err)
	}
	var doc D
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("rpcservertest: %s does not decode as %T: %v", data, doc, err)
	}
	k.Doc = doc
}

// merge applies an RFC 7386 merge patch to target.
func merge(target, patch map[string]any) {
	for key, value := range patch {
		switch value := value.(type) {
		case nil:
			delete(target, key)
		case map[string]any:
			object, isObject := target[key].(map[string]any)
			if !isObject {
				object = map[string]any{}
			}
			merge(object, value)
			target[key] = object
		default:
			target[key] = value
		}
	}
}
