package ipcamrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"greendrake.nz/go/camstream"
	"greendrake.nz/go/camstream/camstreamtest"
	"greendrake.nz/go/dvrip/dvriptest"
	"greendrake.nz/go/ipcam"
	"greendrake.nz/go/ipcam/connection"
	"greendrake.nz/go/ipcam/dvripcam"
	"greendrake.nz/go/ipcam/fleet"
	"greendrake.nz/go/ipcam/ipcamtest"
	"greendrake.nz/go/rpcserver"
	"greendrake.nz/go/rpcserver/rpcservertest"
)

// seed is a camera with a setting of each answer.
var seed = ipcamtest.Seed{
	Specifics: map[ipcam.Key]ipcam.Specifics{"ntp.server": {}, "ntp.port": {}, "network.gateway": {}, "maintenance.rebootHour": {}},
	Values:    map[ipcam.Key]any{"ntp.server": "pool.ntp.org", "ntp.port": 123, "network.gateway": "192.0.2.1", "maintenance.rebootHour": 3},
	Restart:   map[ipcam.Key]bool{"ntp.port": true},
	Refuse:    map[ipcam.Key]string{"maintenance.rebootHour": "107 no permission"},
	Keeps:     map[ipcam.Key]any{"network.gateway": "192.0.2.1"},
}

// dial is the connection to the camera at address through driver, dialled.
func dial(t *testing.T, driver ipcam.Driver, address string) *connection.Connection {
	t.Helper()
	conn, err := connection.Dial(context.Background(), driver, address, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

// pushed is a camera's state, as the fleet pushes it.
type pushed struct {
	name  string
	state connection.State
}

// running is a fleet run for a test, its cameras' states published on its broadcast.
type running struct {
	*fleet.Fleet
	t         *testing.T
	broadcast *rpcserver.Broadcast
	pushes    chan pushed
}

func run(t *testing.T, cameras map[string]fleet.Camera) *running {
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{Fleet: fleet.New(ctx, cameras, ""), t: t, broadcast: rpcserver.NewBroadcast(), pushes: make(chan pushed, 64)}
	publish := Cameras(r.broadcast, r.Fleet)
	r.Start(func(name string, state connection.State) {
		publish(name, state)
		r.pushes <- pushed{name, state}
	})
	t.Cleanup(func() {
		cancel()
		r.Wait()
	})
	return r
}

// await waits for the named camera to be pushed in status.
func (r *running) await(name string, status connection.Status) {
	r.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case p := <-r.pushes:
			if p.name == name && p.state.State == status {
				return
			}
		case <-deadline:
			r.t.Fatalf("%s was not pushed %s", name, status)
		}
	}
}

// harness is a fleet of cameras in memory, "porch" with settings and "gate" with none,
// and the methods on it.
type harness struct {
	*running
	porch   *ipcamtest.Camera
	methods map[string]rpcserver.MethodDef
}

func start(t *testing.T) *harness {
	t.Helper()
	configurable, streamsOnly := ipcamtest.NewSettingsDriver(t), ipcamtest.NewDriver(t)
	porch := configurable.Add("porch", seed)
	streamsOnly.Add("gate", seed)
	r := run(t, map[string]fleet.Camera{"porch": {Conn: dial(t, configurable, "porch")}, "gate": {Conn: dial(t, streamsOnly, "gate")}})
	return &harness{running: r, porch: porch, methods: Methods(r.Fleet, Access{})}
}

// call calls a method as the app does: its arguments as the first of an array.
func (h *harness) call(name string, args any) (any, error) {
	h.t.Helper()
	raw, err := json.Marshal([]any{args})
	if err != nil {
		h.t.Fatal(err)
	}
	return h.methods[name].Handler(&rpcserver.HandlerContext{Ctx: context.Background()}, raw)
}

// apiError is the APIError err answers with.
func apiError(err error) *rpcserver.APIError {
	var apiErr *rpcserver.APIError
	if !errors.As(err, &apiErr) {
		return &rpcserver.APIError{}
	}
	return apiErr
}

// porch is the arguments of a call on the camera "porch", with more.
func porch(more map[string]any) map[string]any {
	args := map[string]any{"Camera": "porch"}
	for key, value := range more {
		args[key] = value
	}
	return args
}

// The host decides what every method asks of its caller.
func TestAccess(t *testing.T) {
	f := run(t, nil).Fleet
	for name, def := range Methods(f, Access{Auth: rpcserver.AuthRequired, Access: "cameras"}) {
		if def.Auth != rpcserver.AuthRequired || def.Access != "cameras" || def.Handler == nil {
			t.Errorf("%s: %+v", name, def)
		}
	}
	if methods := Methods(f, Access{}); len(methods) != 6 || methods["camera.reboot"].Auth != rpcserver.AuthNone {
		t.Errorf("methods %+v", methods)
	}
}

func TestGetSettings(t *testing.T) {
	h := start(t)
	result, err := h.call("settings.groups", porch(nil))
	if err != nil {
		t.Fatal(err)
	}
	if groups := result.([]ipcam.Group); len(groups) != 3 || groups[1] != (ipcam.Group{ID: "ntp", Title: "NTP"}) {
		t.Fatalf("groups %+v", groups)
	}
	result, err = h.call("settings.get", porch(map[string]any{"Group": "ntp"}))
	if err != nil {
		t.Fatal(err)
	}
	if settings := result.([]ipcam.Setting); len(settings) != 2 || settings[0].Key != "ntp.server" || settings[1].Value != 123 {
		t.Fatalf("settings %+v", settings)
	}
	_, err = h.call("settings.get", porch(map[string]any{"Group": "nope"}))
	if got := apiError(err); got.Code != "UNKNOWN_GROUP" || got.Details != (Problem{Message: `There is no group of settings "nope".`}) {
		t.Fatalf("no such group: %v (%+v)", err, got.Details)
	}
}

func TestSetSetting(t *testing.T) {
	h := start(t)
	result, err := h.call("settings.set", porch(map[string]any{"Key": "network.gateway", "Value": "0.0.0.0"}))
	if err != nil {
		t.Fatal(err)
	}
	// What the camera kept, rather than what was written.
	if set := result.(SetResult); set.Restart || len(set.Changed) != 1 || set.Changed[0].Value != "192.0.2.1" || set.Changed[0].Label != "Gateway" {
		t.Fatalf("set %+v", set)
	}
	// A number as the wire has it, written as the setting's kind takes it.
	result, err = h.call("settings.set", porch(map[string]any{"Key": "ntp.port", "Value": 124}))
	if err != nil || !result.(SetResult).Restart {
		t.Fatalf("set %+v, %v", result, err)
	}
	if written := h.porch.Written(); len(written) != 2 || written[1] != (ipcamtest.Write{Key: "ntp.port", Value: 124}) {
		t.Fatalf("written %+v", written)
	}
}

func TestSetRefusals(t *testing.T) {
	h := start(t)
	tests := map[string]struct {
		args    map[string]any
		code    string
		message string
	}{
		"a value of another kind":    {map[string]any{"Key": "ntp.port", "Value": "123"}, "INVALID_VALUE", "Not a whole number from 1 to 65535."},
		"a setting the camera lacks": {map[string]any{"Key": "network.dhcp", "Value": true}, "UNKNOWN_SETTING", `The camera has no setting "network.dhcp".`},
		"a write the camera refuses": {map[string]any{"Key": "maintenance.rebootHour", "Value": 4}, "CAMERA_REFUSED", "The camera refused: 107 no permission"},
	}
	for name, test := range tests {
		_, err := h.call("settings.set", porch(test.args))
		if got := apiError(err); got.Code != test.code || got.Details != (Problem{Message: test.message}) {
			t.Errorf("%s: %v (%+v)", name, err, got.Details)
		}
	}
	if _, err := h.call("settings.set", "not an object"); err == nil {
		t.Error("arguments of the wrong shape went through")
	}
	if _, err := h.call("camera.setTime", porch(map[string]any{"Time": "half past nine"})); apiError(err).Code != rpcserver.CodeInvalidArgument {
		t.Errorf("a clock time of no form: %v", err)
	}
	if written := h.porch.Written(); len(written) != 0 {
		t.Fatalf("written %+v", written)
	}
}

func TestClock(t *testing.T) {
	h := start(t)
	if _, err := h.call("camera.setTime", porch(map[string]any{"Time": "2026-10-06 21:30:00"})); err != nil {
		t.Fatal(err)
	}
	result, err := h.call("camera.time", porch(nil))
	if err != nil || result.(Clock).Time != "2026-10-06 21:30:00" {
		t.Fatalf("clock %v, %v", result, err)
	}
}

func TestOfflineCalls(t *testing.T) {
	h := start(t)
	if _, err := h.call("camera.reboot", porch(nil)); err != nil {
		t.Fatal(err)
	}
	h.await("porch", connection.Offline)
	for _, name := range []string{"camera.time", "settings.get"} {
		if _, err := h.call(name, porch(nil)); apiError(err).Code != "CAMERA_OFFLINE" || apiError(err).Details != (Problem{Message: "The camera is offline."}) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := h.call("settings.set", porch(map[string]any{"Key": "ntp.port", "Value": 123})); apiError(err).Code != "CAMERA_OFFLINE" {
		t.Errorf("a write: %v", err)
	}
}

// Every method names its camera, which the fleet must have, with settings.
func TestCallsOnOtherCameras(t *testing.T) {
	h := start(t)
	for name := range h.methods {
		_, err := h.call(name, map[string]any{"Camera": "shed", "Time": "2026-10-06 21:30:00"})
		if got := apiError(err); got.Code != "UNKNOWN_CAMERA" || got.Details != (Problem{Message: `There is no camera "shed".`}) {
			t.Errorf("%s on no camera: %v (%+v)", name, err, got.Details)
		}
		_, err = h.call(name, map[string]any{"Camera": "gate", "Time": "2026-10-06 21:30:00"})
		if got := apiError(err); got.Code != "NO_SETTINGS" || got.Details != (Problem{Message: "The camera has no settings to change."}) {
			t.Errorf("%s on a camera with no settings: %v (%+v)", name, err, got.Details)
		}
	}
}

// The cameras are a document: every camera's state greets a socket, and a change of one
// camera's state is patched as that change alone.
func TestCameras(t *testing.T) {
	h := start(t)
	server := httptest.NewServer(h.broadcast.Handler(rpcserver.NewDispatcher(h.methods)))
	t.Cleanup(server.Close)
	conn := rpcservertest.Dial(t, server.URL)

	doc, _ := rpcservertest.State(t, conn, StateType)
	porchState, gateState := doc["porch"].(map[string]any), doc["gate"].(map[string]any)
	if len(doc) != 2 || porchState["State"] != "online" || porchState["Settings"] != true || gateState["Settings"] != false {
		t.Fatalf("document %v", doc)
	}

	if _, err := h.call("settings.set", porch(map[string]any{"Key": "ntp.port", "Value": 124})); err != nil {
		t.Fatal(err)
	}
	if _, patch := rpcservertest.State(t, conn, StateType); !reflect.DeepEqual(patch, map[string]any{"porch": map[string]any{"RestartPending": true}}) {
		t.Fatalf("patch %v", patch)
	}
}

// A failure the API has no word for is a fault, left to the dispatcher to log and
// answer INTERNAL_ERROR.
func TestFaults(t *testing.T) {
	broken := errors.New("broken")
	var apiErr *rpcserver.APIError
	if err := answer("settings.get", broken); !errors.Is(err, broken) || errors.As(err, &apiErr) {
		t.Fatalf("got %v", err)
	}
}

// serve serves the streams of a fleet of cameras, and is where a socket reaches them.
func serve(t *testing.T, cameras map[string]fleet.Camera) (*running, string) {
	t.Helper()
	r := run(t, cameras)
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(Streams(ctx, r.Fleet))
	t.Cleanup(func() {
		cancel()
		server.Close()
	})
	return r, "ws" + strings.TrimPrefix(server.URL, "http")
}

// cast serves the streams of a fleet of one camera, "camera", on fake, and opens a socket
// on them.
func cast(t *testing.T, fake *dvriptest.Camera) *websocket.Conn {
	t.Helper()
	r, url := serve(t, map[string]fleet.Camera{"camera": {Conn: dial(t, dvripcam.Driver{}, fake.Address())}})
	r.await("camera", connection.Online)
	ws, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadLimit(-1)
	t.Cleanup(func() { ws.CloseNow() })
	return ws
}

func send(t *testing.T, ws *websocket.Conn, op map[string]any) {
	t.Helper()
	data, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

// receive reads the next message: a text one decoded, a binary one as it is.
func receive(t *testing.T, ws *websocket.Conn) (map[string]any, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kind, message, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if kind == websocket.MessageBinary {
		return nil, message
	}
	var op map[string]any
	if err := json.Unmarshal(message, &op); err != nil {
		t.Fatal(err)
	}
	return op, nil
}

// A watched stream is pulled off the camera and cast: an initialization segment, then
// media; unwatched, it is stopped on the camera.
func TestAStreamIsCast(t *testing.T) {
	fake := dvriptest.New(t, "techage", dvriptest.Options{})
	ws := cast(t, fake)
	send(t, ws, map[string]any{"op": "sub", "id": 1, "key": "camera/extra"})
	if op, _ := receive(t, ws); op["op"] != "subbed" {
		t.Fatalf("answered %v", op)
	}
	if _, init := receive(t, ws); !bytes.Contains(init, []byte("moov")) {
		t.Fatalf("first payload % x…", init[:min(len(init), 16)])
	}
	if _, media := receive(t, ws); !bytes.Contains(media, []byte("moof")) {
		t.Fatalf("second payload % x…", media[:min(len(media), 16)])
	}
	ws.CloseNow()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, command := range fake.Commands("OPMonitor") {
			var op struct{ Action string }
			if command.Payload(&op) == nil && op.Action == "Stop" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the stream was not stopped on the camera")
}

// Keys name a camera's stream; a stream the camera encodes in another codec than the
// cast one is refused, and the camera is not asked for it.
func TestStreamsItCannotServeAreRefused(t *testing.T) {
	fake := dvriptest.New(t, "techage", dvriptest.Options{})
	if err := fake.Overlay("Simplify.Encode", json.RawMessage(`[{"ExtraFormat":{"Video":{"Compression":"H.264"}}}]`)); err != nil {
		t.Fatal(err)
	}
	ws := cast(t, fake)
	for i, key := range []string{"camera/extra", "camera/nope", "shed/main", "camera"} {
		send(t, ws, map[string]any{"op": "sub", "id": i, "key": key})
		if op, _ := receive(t, ws); op["op"] != "error" {
			t.Fatalf("%s answered %v", key, op)
		}
	}
	if claims := fake.Commands("OPMonitor"); len(claims) != 0 {
		t.Fatalf("the camera was asked for it: %+v", claims)
	}
}

// A camera's streams relayed off another host are watched here, and not relayed on.
func TestRelayedStreamsAreNotRelayedOn(t *testing.T) {
	driver := ipcamtest.NewDriver(t)
	driver.Add("porch", ipcamtest.Seed{Streams: map[ipcam.StreamKey]ipcam.StreamFormat{ipcam.Main: {Codec: ipcam.H265}}})
	cam := camstreamtest.NewCamera(t)
	_, url := serve(t, map[string]fleet.Camera{"porch": {Conn: dial(t, driver, "porch"), StreamsFrom: func(ctx context.Context, _ ipcam.StreamKey) (camstream.Monitor, error) {
		return cam.Open(ctx)
	}}})
	ws, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	send(t, ws, map[string]any{"op": "sub", "id": 1, "key": "porch/main", "raw": true})
	if op, _ := receive(t, ws); op["op"] != "error" {
		t.Fatalf("a relay answered %v", op)
	}
	send(t, ws, map[string]any{"op": "sub", "id": 2, "key": "porch/main"})
	if op, _ := receive(t, ws); op["op"] != "subbed" {
		t.Fatalf("a viewer answered %v", op)
	}
}

// A camera whose streams come from another host's camera, Upstream, plays them off that
// host, and never opens them on its own login, which it is logged in to all the same.
func TestUpstream(t *testing.T) {
	gate, porch := dvriptest.New(t, "techage", dvriptest.Options{}), dvriptest.New(t, "techage", dvriptest.Options{})
	_, url := serve(t, map[string]fleet.Camera{"gate": {Conn: dial(t, dvripcam.Driver{}, gate.Address())}})
	r := run(t, map[string]fleet.Camera{"porch": {Conn: dial(t, dvripcam.Driver{}, porch.Address()), StreamsFrom: Upstream(url, "gate")}})
	source, relayed, ok := r.Source("porch", ipcam.Extra)
	if !ok || !relayed {
		t.Fatalf("source %v, relayed %v", ok, relayed)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	select {
	case f := <-source.Subscribe(ctx).Frames():
		if f.Kind != camstream.Video || !f.Keyframe {
			t.Fatalf("the first frame %+v", f)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no frame came")
	}
	if len(gate.Commands("OPMonitor")) == 0 || len(porch.Commands("OPMonitor")) != 0 {
		t.Fatalf("the stream was opened on the other host's camera %d times, on the camera's own login %d", len(gate.Commands("OPMonitor")), len(porch.Commands("OPMonitor")))
	}
}
