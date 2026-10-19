package connection_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"greendrake.nz/go/ipcam"
	"greendrake.nz/go/ipcam/connection"
	"greendrake.nz/go/ipcam/internal/testcamera"
	"greendrake.nz/go/ipcam/ipcamtest"
)

// seed is a camera with a setting of each answer.
var seed = ipcamtest.Seed{
	Specifics: map[ipcam.Key]ipcam.Specifics{
		"ntp.server":                             {},
		"network.gateway":                        {},
		"network.address":                        {},
		ipcam.StreamSetting(ipcam.Main, "codec"): {},
		"maintenance.rebootHour":                 {},
	},
	Values: map[ipcam.Key]any{
		"ntp.server":                             "pool.ntp.org",
		"network.gateway":                        "192.0.2.1",
		"network.address":                        "192.0.2.167",
		ipcam.StreamSetting(ipcam.Main, "codec"): "H.265",
		"maintenance.rebootHour":                 3,
	},
	Streams: map[ipcam.StreamKey]ipcam.StreamFormat{ipcam.Main: {Codec: ipcam.H265, Audio: true}, ipcam.Extra: {Codec: ipcam.H264}},
	Restart: map[ipcam.Key]bool{ipcam.StreamSetting(ipcam.Main, "codec"): true},
	Refuse:  map[ipcam.Key]string{"maintenance.rebootHour": "107 no permission"},
	Keeps:   map[ipcam.Key]any{"network.gateway": "192.0.2.1"},
	Moves:   map[ipcam.Key]string{"network.address": "moved"},
}

// start connects to a camera seeded so at "camera".
func start(t *testing.T) (*testcamera.Harness, ipcamtest.SettingsDriver, *ipcamtest.Camera) {
	driver := ipcamtest.NewSettingsDriver(t)
	cam := driver.Add("camera", seed)
	return testcamera.Dial(t, driver, "camera"), driver, cam
}

func reboot(t *testing.T, h *testcamera.Harness) {
	t.Helper()
	testcamera.Reboot(t, h.Conn)
	if offline := h.Await("offline", testcamera.Offline); offline.Error == "" {
		t.Fatal("offline with no reason")
	}
}

func TestDialReadsTheCamera(t *testing.T) {
	h, _, _ := start(t)
	state := h.Await("online", testcamera.Online)
	if state.Address != "camera" || !state.Settings || state.RestartPending || state.Cast != ipcam.H265 ||
		state.Streams[ipcam.Main] != (connection.StreamState{Codec: ipcam.H265, Cast: true}) || state.Streams[ipcam.Extra] != (connection.StreamState{Codec: ipcam.H264}) {
		t.Fatalf("state %+v", state)
	}
	if now := h.Conn.State(); now.State != connection.Online {
		t.Fatalf("state now %+v", now)
	}
	if cam, err := h.Conn.Await(context.Background()); err != nil || cam == nil {
		t.Fatalf("awaited %v, %v", cam, err)
	}
}

func TestDialFails(t *testing.T) {
	for name, driver := range map[string]ipcam.Driver{"with settings": ipcamtest.NewSettingsDriver(t), "without": ipcamtest.NewDriver(t)} {
		if _, err := connection.Dial(context.Background(), driver, "nowhere", "admin", ""); !errors.Is(err, ipcam.ErrUnreachable) {
			t.Fatalf("%s: dialled nothing: %v", name, err)
		}
	}
}

// A started connection is offline until it has dialled the camera, which it does at once.
func TestStartBeginsOffline(t *testing.T) {
	driver := ipcamtest.NewSettingsDriver(t)
	driver.Add("camera", seed)
	h := testcamera.Start(t, driver, "camera")
	if first := h.Await("first", func(connection.State) bool { return true }); first.State != connection.Offline || first.Error != "" || first.Address != "camera" || !first.Settings || first.Streams != nil {
		t.Fatalf("started as %+v", first)
	}
	h.Await("online", testcamera.Online)
}

// A refused password leaves the camera offline, saying so, and is not tried again.
func TestRefusedPasswordStopsTheDialling(t *testing.T) {
	driver := ipcamtest.NewSettingsDriver(t)
	cam := driver.Add("camera", ipcamtest.Seed{Refuses: true})
	h := testcamera.Start(t, driver, "camera")
	h.Await("refused", func(s connection.State) bool {
		return testcamera.Offline(s) && strings.Contains(s.Error, "wrong password")
	})
	// Past the second a dial that failed otherwise is tried again after.
	time.Sleep(1500 * time.Millisecond)
	if dials := cam.Dials(); dials != 1 {
		t.Fatalf("dialled %d times", dials)
	}
}

// A reboot takes the camera offline until it is back; the connection dials it again,
// and the next login clears a pending restart.
func TestRedialAfterAReboot(t *testing.T) {
	h, _, cam := start(t)
	if restart, _, err := h.Conn.Apply(context.Background(), ipcam.StreamSetting(ipcam.Main, "codec"), "H.265"); err != nil || !restart {
		t.Fatalf("restart %v, %v", restart, err)
	}
	h.Await("with a restart pending", func(s connection.State) bool { return s.RestartPending })
	reboot(t, h)
	if _, err := h.Conn.Configurable(); err != connection.ErrOffline {
		t.Fatalf("settings while offline: %v", err)
	}
	if _, err := h.Conn.Settings(context.Background(), "ntp"); err != connection.ErrOffline {
		t.Fatalf("a group while offline: %v", err)
	}
	if _, err := h.Conn.Groups(context.Background()); err != connection.ErrOffline {
		t.Fatalf("groups while offline: %v", err)
	}
	if _, _, err := h.Conn.Apply(context.Background(), "ntp.server", "x"); err != connection.ErrOffline {
		t.Fatalf("a write while offline: %v", err)
	}
	waited := make(chan ipcam.Camera)
	go func() {
		cam, _ := h.Conn.Await(context.Background())
		waited <- cam
	}()
	h.Await("dialling the camera while it is away", func(s connection.State) bool {
		return testcamera.Offline(s) && strings.Contains(s.Error, "rebooting")
	})
	cam.Return()
	if online := h.Await("online again", testcamera.Online); online.RestartPending || online.Error != "" {
		t.Fatalf("online as %+v", online)
	}
	select {
	case cam := <-waited:
		if cam == nil {
			t.Fatal("awaited no camera")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Await did not wake")
	}
}

func TestAwaitEndsWithItsContext(t *testing.T) {
	h, _, _ := start(t)
	reboot(t, h)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := h.Conn.Await(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("await got %v", err)
	}
}

// A write that moves the camera has the connection dial it where it went, once the
// login on the old address ends.
func TestMoveFollowsTheCamera(t *testing.T) {
	h, driver, _ := start(t)
	driver.Add("moved", ipcamtest.Seed{})
	if _, _, err := h.Conn.Apply(context.Background(), "network.address", "192.0.2.168"); err != nil {
		t.Fatal(err)
	}
	reboot(t, h)
	h.Await("online where it moved", func(s connection.State) bool { return testcamera.Online(s) && s.Address == "moved" })
}

// A group's settings are described from the values of its settings alone.
func TestSettings(t *testing.T) {
	h, _, _ := start(t)
	ctx := context.Background()
	groups, err := h.Conn.Groups(ctx)
	if err != nil || len(groups) != 4 || groups[0].ID != "encode" || groups[3].ID != "maintenance" {
		t.Fatalf("groups %+v, %v", groups, err)
	}
	settings, err := h.Conn.Settings(ctx, "network")
	if err != nil {
		t.Fatal(err)
	}
	if len(settings) != 2 || settings[0].Key != "network.address" || settings[1].Key != "network.gateway" || settings[1].Value != "192.0.2.1" || settings[1].Kind.Type != ipcam.IPv4 {
		t.Fatalf("settings %+v", settings)
	}
	var unknown *ipcam.UnknownGroup
	if _, err := h.Conn.Settings(ctx, "nope"); !errors.As(err, &unknown) {
		t.Fatalf("no such group: %v", err)
	}
}

// A write is checked before it reaches the camera, and answers what the camera has
// once it is written.
func TestApply(t *testing.T) {
	h, _, cam := start(t)
	ctx := context.Background()
	restart, changed, err := h.Conn.Apply(ctx, "network.gateway", "0.0.0.0")
	if err != nil || restart || len(changed) != 1 || changed[0].Key != "network.gateway" || changed[0].Value != "192.0.2.1" || changed[0].Label != "Gateway" {
		t.Fatalf("restart %v, changed %+v, %v", restart, changed, err)
	}
	var invalid *ipcam.InvalidValue
	if _, _, err := h.Conn.Apply(ctx, "network.gateway", "256.0.0.0"); !errors.As(err, &invalid) {
		t.Fatalf("an address of no form: %v", err)
	}
	if _, _, err := h.Conn.Apply(ctx, ipcam.StreamSetting(ipcam.Main, "codec"), "H.264"); !errors.As(err, &invalid) {
		t.Fatalf("a pinned setting written otherwise: %v", err)
	}
	var unknown *ipcam.UnknownSetting
	if _, _, err := h.Conn.Apply(ctx, "network.dhcp", true); !errors.As(err, &unknown) {
		t.Fatalf("a setting the camera does not have: %v", err)
	}
	var refused *ipcam.Refused
	if _, _, err := h.Conn.Apply(ctx, "maintenance.rebootHour", 4.0); !errors.As(err, &refused) || refused.Message != "107 no permission" {
		t.Fatalf("refused: %v", err)
	}
	if written := cam.Written(); len(written) != 1 || written[0] != (ipcamtest.Write{Key: "network.gateway", Value: "0.0.0.0"}) {
		t.Fatalf("written %+v", written)
	}
	if state := h.Conn.State(); state.RestartPending {
		t.Fatal("a restart pending with none answered")
	}
}

// A camera whose driver has no settings says so, online or not.
func TestNoSettings(t *testing.T) {
	driver := ipcamtest.NewDriver(t)
	driver.Add("camera", seed)
	h := testcamera.Dial(t, driver, "camera")
	if state := h.Await("online", testcamera.Online); state.Settings {
		t.Fatalf("state %+v", state)
	}
	ctx := context.Background()
	if _, err := h.Conn.Configurable(); err != connection.ErrNoSettings {
		t.Fatalf("settings: %v", err)
	}
	if _, err := h.Conn.Groups(ctx); err != connection.ErrNoSettings {
		t.Fatalf("groups: %v", err)
	}
	if _, err := h.Conn.Settings(ctx, "ntp"); err != connection.ErrNoSettings {
		t.Fatalf("a group: %v", err)
	}
	if _, _, err := h.Conn.Apply(ctx, "ntp.server", "x"); err != connection.ErrNoSettings {
		t.Fatalf("a write: %v", err)
	}
}
