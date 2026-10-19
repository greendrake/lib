package fleet_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"greendrake.nz/go/camstream"
	"greendrake.nz/go/camstream/camstreamtest"
	"greendrake.nz/go/dvrip/dvriptest"
	"greendrake.nz/go/ipcam"
	"greendrake.nz/go/ipcam/connection"
	"greendrake.nz/go/ipcam/dvripcam"
	"greendrake.nz/go/ipcam/fleet"
	"greendrake.nz/go/ipcam/internal/testcamera"
	"greendrake.nz/go/ipcam/ipcamtest"
)

// fake is a camera that keeps away for a moment when rebooted.
func fake(t *testing.T) *dvriptest.Camera {
	return dvriptest.New(t, "techage", dvriptest.Options{RebootPause: 200 * time.Millisecond})
}

func dial(t *testing.T, camera *dvriptest.Camera) *connection.Connection {
	t.Helper()
	return testcamera.Conn(t, dvripcam.Driver{}, camera.Address())
}

// reboot reboots the named camera, which the fleet then has offline.
func reboot(t *testing.T, f *testcamera.Fleet, name string) {
	t.Helper()
	conn, _ := f.Conn(name)
	testcamera.Reboot(t, conn)
	f.Await("offline", testcamera.Of(name, testcamera.Offline))
}

// source is the fleet's source of the named camera's stream, which it must have.
func source(t *testing.T, f *testcamera.Fleet, name string, key ipcam.StreamKey) *camstream.Source {
	t.Helper()
	s, _, ok := f.Source(name, key)
	if !ok {
		t.Fatalf("no source of %s's %s", name, key)
	}
	return s
}

// watch subscribes to source for the test, and awaits a frame of it.
func watch(t *testing.T, source *camstream.Source) *camstream.Frame {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	select {
	case f := <-source.Subscribe(ctx).Frames():
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame came")
	}
	return nil
}

// Every change of a camera's state is pushed by its name; the fleet has every camera's
// state by name.
func TestCamerasAreNamed(t *testing.T) {
	porch, gate := fake(t), fake(t)
	f := testcamera.RunFleet(t, map[string]fleet.Camera{"porch": {Conn: dial(t, porch)}, "gate": {Conn: dial(t, gate)}}, "")
	online := map[string]bool{}
	f.Await("both online", func(p testcamera.Pushed) bool {
		online[p.Name] = online[p.Name] || testcamera.Online(p.State)
		return online["porch"] && online["gate"]
	})
	reboot(t, f, "gate")
	if states := f.States(); len(states) != 2 || states["porch"].Address != porch.Address() || states["gate"].State != connection.Offline {
		t.Fatalf("states %+v", states)
	}
	for _, at := range []struct {
		name string
		key  ipcam.StreamKey
	}{{"shed", ipcam.Main}, {"porch", "nope"}} {
		if _, _, ok := f.Source(at.name, at.key); ok {
			t.Fatalf("%s's %s: a camera or stream the fleet has not", at.name, at.key)
		}
	}
	if _, relayed, _ := f.Source("porch", ipcam.Main); relayed {
		t.Fatal("a camera's own stream is relayed")
	}
	if _, known := f.Conn("shed"); known {
		t.Fatal("a camera the fleet has not")
	}
}

// A stream subscribed to while its camera is offline opens once it is back.
func TestStreamWaitsOutTheCameraOffline(t *testing.T) {
	f := testcamera.RunFleet(t, map[string]fleet.Camera{"porch": {Conn: dial(t, fake(t))}}, "")
	reboot(t, f, "porch")
	watch(t, source(t, f, "porch", ipcam.Extra))
}

// encode has the camera encode its extra stream in codec, from its next login on.
func encode(t *testing.T, camera *dvriptest.Camera, codec ipcam.Codec) {
	t.Helper()
	if err := camera.Overlay("Simplify.Encode", json.RawMessage(`[{"ExtraFormat":{"Video":{"Compression":"`+string(codec)+`"}}}]`)); err != nil {
		t.Fatal(err)
	}
}

// A stream on another codec than the cast one is not opened: it has no source, and its
// recorder waits for the login the camera encodes it in the cast codec at.
func TestStreamOnAnotherCodecWaitsForTheNextLogin(t *testing.T) {
	camera := fake(t)
	encode(t, camera, ipcam.H264)
	dir := t.TempDir()
	f := testcamera.RunFleet(t, map[string]fleet.Camera{"porch": {Conn: dial(t, camera), Record: []ipcam.StreamKey{ipcam.Extra}}}, dir)
	f.Await("online", testcamera.Of("porch", testcamera.Online))
	if _, _, ok := f.Source("porch", ipcam.Extra); ok {
		t.Fatal("a stream on H.264 has a source")
	}
	encode(t, camera, ipcam.H265)
	reboot(t, f, "porch")
	f.Await("online again", func(p testcamera.Pushed) bool { return testcamera.Online(p.State) && p.State.Streams[ipcam.Extra].Cast })
	deadline := time.Now().Add(5 * time.Second)
	for {
		recordings, err := filepath.Glob(filepath.Join(dir, "porch", "*", "*", "*", "*.extra.mkv"))
		if err != nil {
			t.Fatal(err)
		}
		if len(recordings) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stream was not recorded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The first login, on H.264, was never asked for the stream: the reboot was its.
	first := string(camera.Commands("OPMachine")[0].Body["SessionID"])
	for _, command := range camera.Commands("OPMonitor") {
		if string(command.Body["SessionID"]) == first {
			t.Fatalf("the login on H.264 was asked for the stream: %+v", command)
		}
	}
}

// streamsOnly is a camera in memory with both streams in the cast codec and no settings,
// whose stream opening fails the test: the streams of a camera with a StreamsFrom.
func streamsOnly(t *testing.T) *connection.Connection {
	driver := ipcamtest.NewDriver(t)
	driver.Add("porch", ipcamtest.Seed{Streams: map[ipcam.StreamKey]ipcam.StreamFormat{ipcam.Main: {Codec: ipcam.H265}, ipcam.Extra: {Codec: ipcam.H265}}})
	return testcamera.Conn(t, driver, "porch")
}

// A camera whose streams come from elsewhere plays them from there, and never opens them
// on its own login; its source says they are relayed.
func TestStreamsFrom(t *testing.T) {
	cam := camstreamtest.NewCamera(t)
	asked := make(chan ipcam.StreamKey, 16)
	f := testcamera.RunFleet(t, map[string]fleet.Camera{"porch": {Conn: streamsOnly(t), StreamsFrom: func(ctx context.Context, key ipcam.StreamKey) (camstream.Monitor, error) {
		asked <- key
		return cam.Open(ctx)
	}}}, "")
	s, relayed, ok := f.Source("porch", ipcam.Main)
	if !ok || !relayed {
		t.Fatalf("source %v, relayed %v", ok, relayed)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sub := s.Subscribe(ctx)
	m := camstreamtest.NewMonitor()
	cam.Opens(m)
	if key := <-asked; key != ipcam.Main {
		t.Fatalf("asked for %s", key)
	}
	sent := &camstream.Frame{Kind: camstream.Video, Codec: camstream.H265, Keyframe: true, PTS: time.Second, Duration: 40 * time.Millisecond, Data: []byte{0, 0, 0, 1, 38, 1, 0xaf}}
	m.Frames <- sent
	select {
	case got := <-sub.Frames():
		if !reflect.DeepEqual(got, sent) {
			t.Fatalf("played %+v, want %+v", got, sent)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no frame came")
	}
}

// lines is what is written to it, line by line, as the log writes them.
type lines struct {
	mu      sync.Mutex
	written []string
}

func (l *lines) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.written = append(l.written, string(b))
	return len(b), nil
}

func (l *lines) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.written)
}

// A stream that fails to open says why, once for as long as it fails for the same reason.
func TestFailedOpensAreLoggedOnce(t *testing.T) {
	logged := &lines{}
	asked := make(chan struct{}, 16)
	f := testcamera.RunFleet(t, map[string]fleet.Camera{"porch": {Conn: streamsOnly(t), StreamsFrom: func(context.Context, ipcam.StreamKey) (camstream.Monitor, error) {
		asked <- struct{}{}
		return nil, errors.New("no camera shed there")
	}}}, "", fleet.WithLogger(slog.New(slog.NewTextHandler(logged, nil))))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	source(t, f, "porch", ipcam.Main).Subscribe(ctx)
	// The third ask is made once the second attempt, and whatever it logged, is over.
	for range 3 {
		select {
		case <-asked:
		case <-time.After(10 * time.Second):
			t.Fatal("the stream was not tried again")
		}
	}
	if all := logged.all(); len(all) != 1 || !strings.Contains(all[0], "camera=porch stream=main") || !strings.Contains(all[0], "no camera shed there") {
		t.Fatalf("logged %q", all)
	}
}

// A stream from elsewhere waits for the login that encodes it in the cast codec, as one
// off the camera does.
func TestStreamsFromWaitForTheCastCodec(t *testing.T) {
	camera := fake(t)
	encode(t, camera, ipcam.H264)
	conn := dial(t, camera)
	// Whether the login the camera is on as the stream is asked for encodes it in the cast
	// codec.
	cast := make(chan bool, 16)
	f := testcamera.RunFleet(t, map[string]fleet.Camera{"porch": {Conn: conn, Record: []ipcam.StreamKey{ipcam.Extra}, StreamsFrom: func(ctx context.Context, key ipcam.StreamKey) (camstream.Monitor, error) {
		cast <- conn.State().Streams[key].Cast
		<-ctx.Done()
		return nil, ctx.Err()
	}}}, t.TempDir())
	f.Await("online", testcamera.Of("porch", testcamera.Online))
	encode(t, camera, ipcam.H265)
	reboot(t, f, "porch")
	select {
	case cast := <-cast:
		if !cast {
			t.Fatal("the stream was asked for on the login that encodes it in H.264")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream on H.265 was not asked for")
	}
}
