package dvriptest

import (
	"encoding/json"
	"reflect"
	"testing"

	"greendrake.nz/go/dvrip/internal/wire"
)

// New starts a camera answering as device for t, stopped with it. What the camera finds
// wrong in what it is sent fails t.
func New(t testing.TB, device string, options Options) *Camera {
	t.Helper()
	camera, err := Start(device, t.Errorf, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(camera.Close)
	return camera
}

// SameJSON reports whether two JSON documents carry the same content, numbers exactly
// as written: what a camera was sent against what it holds.
func SameJSON(t testing.TB, a, b json.RawMessage) bool {
	t.Helper()
	decode := func(data json.RawMessage) any {
		v, err := wire.DecodeExact(data)
		if err != nil {
			t.Fatalf("decoding %s: %v", data, err)
		}
		return v
	}
	return reflect.DeepEqual(decode(a), decode(b))
}
