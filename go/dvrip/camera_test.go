package dvrip

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"greendrake.nz/go/dvrip/dvriptest"
	"greendrake.nz/go/dvrip/internal/wire"
)

// recordedDevices are the devices dvriptest has the replies of; tests run against the
// first unless they say otherwise.
var recordedDevices = []string{"techage", "techage-hi3516ev100", "xm530"}

// dial logs in to the camera with the account it ships with.
func dial(t *testing.T, camera *dvriptest.Camera, options ...Option) *Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s, err := Dial(ctx, camera.Address(), "admin", "", options...)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// payload decodes what a command the camera received carried.
func payload(t *testing.T, command dvriptest.Command, v any) {
	t.Helper()
	if err := command.Payload(v); err != nil {
		t.Fatalf("decoding %s: %v", command.Name, err)
	}
}

// lastSet is what the camera was last told to write under name.
func lastSet(t *testing.T, camera *dvriptest.Camera, name string) json.RawMessage {
	t.Helper()
	var body json.RawMessage
	for _, command := range camera.Commands(name) {
		if wire.Code(command.Code) == wire.ConfigSet {
			body = command.Body[name]
		}
	}
	if body == nil {
		t.Fatalf("nothing was written to %s", name)
	}
	return body
}

func replies(t *testing.T, device string) map[string]json.RawMessage {
	t.Helper()
	replies, err := dvriptest.Replies(device)
	if err != nil {
		t.Fatal(err)
	}
	return replies
}
