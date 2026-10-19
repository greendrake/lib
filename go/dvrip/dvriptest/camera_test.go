package dvriptest_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"greendrake.nz/go/dvrip"
	"greendrake.nz/go/dvrip/dvriptest"
	"greendrake.nz/go/dvrip/internal/wire"
)

// reports collects what a camera finds wrong.
type reports struct {
	mu   sync.Mutex
	seen []string
}

func (r *reports) report(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, fmt.Sprintf(format, args...))
}

// await waits for a report mentioning what.
func (r *reports) await(t *testing.T, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, seen := range r.seen {
			if strings.Contains(seen, what) {
				r.mu.Unlock()
				return
			}
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("nothing reported about %q", what)
}

func dial(t *testing.T, camera *dvriptest.Camera, options ...dvrip.Option) *dvrip.Session {
	t.Helper()
	s, err := dvrip.Dial(context.Background(), camera.Address(), "admin", "", options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// keyframe and pframe are video frames as devices send them.
func keyframe(fps byte, data string) []byte {
	b := []byte{0, 0, 1, wire.FrameVideoI, 0x03, fps, 80, 45, 0, 0, 0, 0}
	return append(binary.LittleEndian.AppendUint32(b, uint32(len(data))), data...)
}

func pframe(data string) []byte {
	return append(binary.LittleEndian.AppendUint32([]byte{0, 0, 1, wire.FrameVideoP}, uint32(len(data))), data...)
}

// In real time, the media goes out at its frame rate, and over again once through.
func TestRealtimeMediaLoopsAtItsRate(t *testing.T) {
	media := append(keyframe(25, "key"), pframe("p")...)
	s := dial(t, dvriptest.New(t, "techage", dvriptest.Options{Media: media, Realtime: true}))
	m, err := s.Monitor(context.Background(), 0, dvrip.Main)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	began := time.Now()
	for i := range 6 {
		f, err := m.Next()
		if err != nil {
			t.Fatal(err)
		}
		if want := i%2 == 0; f.Keyframe != want || f.PTS != time.Duration(i)*40*time.Millisecond {
			t.Fatalf("frame %d: key %v at %v", i, f.Keyframe, f.PTS)
		}
	}
	if elapsed := time.Since(began); elapsed < 5*40*time.Millisecond {
		t.Fatalf("six frames at 25 a second came in %v", elapsed)
	}
}

// The recording plays.
func TestRecordingPlays(t *testing.T) {
	s := dial(t, dvriptest.New(t, "techage", dvriptest.Options{Realtime: true}))
	m, err := s.Monitor(context.Background(), 0, dvrip.Extra)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	if f, err := m.Next(); err != nil || !f.Keyframe || f.Width != 800 || f.FPS != 15 {
		t.Fatalf("opened with %+v, %v", f.Kind, err)
	}
}

// A reboot drops every session, and the camera refuses connections for its pause.
func TestRebootKeepsTheCameraAway(t *testing.T) {
	const pause = 300 * time.Millisecond
	camera := dvriptest.New(t, "techage", dvriptest.Options{RebootPause: pause})
	s := dial(t, camera)
	rebooted := time.Now()
	if err := s.Reboot(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-s.Done()
	if _, err := dvrip.Dial(context.Background(), camera.Address(), "admin", ""); err == nil {
		t.Fatal("a rebooting camera took a login")
	}
	for {
		again, err := dvrip.Dial(context.Background(), camera.Address(), "admin", "")
		if err == nil {
			again.Close()
			break
		}
		if time.Since(rebooted) > 10*pause {
			t.Fatalf("still away: %v", err)
		}
		time.Sleep(pause / 10)
	}
	if away := time.Since(rebooted); away < pause {
		t.Fatalf("back after %v", away)
	}
}

// A write the camera restarts on is answered, and then ends the session as a reboot does.
func TestWritesThatRestart(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{Restarts: map[string]bool{dvrip.NetCommon.Name: true}, RebootPause: time.Hour})
	s := dial(t, camera)
	ctx := context.Background()
	common, err := dvrip.NetCommon.Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dvrip.NetCommon.Set(ctx, s, common); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the session outlived the restart")
	}
	if _, err := dvrip.Dial(ctx, camera.Address(), "admin", ""); err == nil {
		t.Fatal("a restarting camera took a login")
	}
}

// A command held is answered once released.
func TestHeldCommands(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{})
	s := dial(t, camera)
	camera.Hold(dvrip.NetNTP.Name)
	answered := make(chan error, 1)
	go func() {
		_, err := dvrip.NetNTP.Get(context.Background(), s)
		answered <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(camera.Commands(dvrip.NetNTP.Name)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the command never arrived")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-answered:
		t.Fatalf("answered while held: %v", err)
	default:
	}
	camera.Release(dvrip.NetNTP.Name)
	if err := <-answered; err != nil {
		t.Fatal(err)
	}
	if _, err := dvrip.NetNTP.Get(context.Background(), s); err != nil {
		t.Fatalf("a command after the release: %v", err)
	}
}

// A write the camera ignores is answered as taken, and reads back as it was.
func TestIgnoredWrites(t *testing.T) {
	s := dial(t, dvriptest.New(t, "techage", dvriptest.Options{Ignores: map[string]bool{dvrip.NetNTP.Name: true}}))
	ctx := context.Background()
	ntp, err := dvrip.NetNTP.Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	written := ntp
	written.Server.Port++
	if _, err := dvrip.NetNTP.Set(ctx, s, written); err != nil {
		t.Fatal(err)
	}
	if now, err := dvrip.NetNTP.Get(ctx, s); err != nil || now != ntp {
		t.Fatalf("read back %+v, %v; want %+v", now, err, ntp)
	}
}

// A configuration changed while the camera runs reads back changed, and only where the
// change says.
func TestOverlay(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{})
	if err := camera.Overlay(dvrip.SimplifyEncode.Name, json.RawMessage(`[{"ExtraFormat":{"Video":{"Compression":"H.264"}}}]`)); err != nil {
		t.Fatal(err)
	}
	encode, err := dvrip.SimplifyEncode.Get(context.Background(), dial(t, camera))
	if err != nil {
		t.Fatal(err)
	}
	if extra, main := encode[0].ExtraFormat.Video, encode[0].MainFormat.Video; extra.Compression != "H.264" || extra.Resolution != "HD1" || main.Compression != "H.265" {
		t.Fatalf("read %+v and %+v", extra, main)
	}
	if err := camera.Overlay("No.Such", json.RawMessage(`{}`)); err == nil {
		t.Fatal("a configuration the camera does not keep changed")
	}
	if err := camera.Overlay(dvrip.SimplifyEncode.Name, json.RawMessage(`{`)); err == nil {
		t.Fatal("a malformed change was laid")
	}
}

func TestStartRefuses(t *testing.T) {
	if _, err := dvriptest.Start("nope", t.Errorf, dvriptest.Options{}); err == nil {
		t.Fatal("an unrecorded device started")
	}
	for _, clock := range []string{`1`, `"noon"`} {
		if _, err := dvriptest.Start("techage", t.Errorf, dvriptest.Options{Replies: map[string]json.RawMessage{"OPTimeQuery": json.RawMessage(clock)}}); err == nil {
			t.Fatalf("a clock of %s started", clock)
		}
	}
	if _, err := dvriptest.Listen("nowhere:x", "techage", t.Errorf, dvriptest.Options{}); err == nil {
		t.Fatal("an unusable address started")
	}
}

// client speaks the protocol raw, to send what dvrip never would.
type client struct {
	t       *testing.T
	conn    net.Conn
	session uint32
}

func connect(t *testing.T, camera *dvriptest.Camera) *client {
	t.Helper()
	conn, err := net.Dial("tcp", camera.Address())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &client{t: t, conn: conn}
}

func (c *client) send(code wire.Code, body []byte) {
	c.t.Helper()
	if _, err := c.conn.Write(wire.AppendPacket(nil, wire.Header{Session: c.session, Code: code}, body)); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) command(code wire.Code, body map[string]any) {
	c.t.Helper()
	data, err := wire.EncodeJSON(body)
	if err != nil {
		c.t.Fatal(err)
	}
	c.send(code, data)
}

// login logs the connection in and reads the session it was given.
func (c *client) login() {
	c.t.Helper()
	c.command(wire.Login, map[string]any{"UserName": "admin", "PassWord": wire.SofiaHash("")})
	p, err := wire.ReadMessage(c.conn)
	if err != nil {
		c.t.Fatal(err)
	}
	var reply struct{ SessionID string }
	if err := json.Unmarshal(wire.TrimJSON(p.Data), &reply); err != nil {
		c.t.Fatal(err)
	}
	if _, err := fmt.Sscanf(reply.SessionID, "0x%x", &c.session); err != nil {
		c.t.Fatal(err)
	}
}

func monitorOp(action, stream string) map[string]any {
	return map[string]any{"Action": action, "Parameter": map[string]any{"Channel": 0, "StreamType": stream}}
}

func TestViolationsAreReported(t *testing.T) {
	var seen reports
	// Its reports are what this test checks, so they go to seen rather than failing it.
	camera, err := dvriptest.Start("techage", seen.report, dvriptest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(camera.Close)

	connect(t, camera).command(wire.KeepAlive, map[string]any{"Name": "KeepAlive"})
	seen.await(t, "opened with message 1006")

	connect(t, camera).send(wire.Login, []byte("not json"))
	seen.await(t, "login body")

	control := connect(t, camera)
	control.login()
	control.send(wire.ConfigGet, []byte("not json"))
	seen.await(t, "message 1042 body")
	control.command(wire.ConfigGet, map[string]any{"Name": 5})
	seen.await(t, "message 1042 name")
	control.command(wire.SystemManager, map[string]any{"Name": "OPTimeSetting", "OPTimeSetting": 5})
	seen.await(t, "OPTimeSetting: json")
	control.command(wire.SystemManager, map[string]any{"Name": "OPTimeSetting", "OPTimeSetting": "noon"})
	seen.await(t, "OPTimeSetting: parsing time")
	control.command(wire.Monitor, map[string]any{"Name": "OPMonitor", "OPMonitor": monitorOp("Start", "Main")})
	seen.await(t, fmt.Sprintf("0x%08X/0/Main started without a claim", control.session))
	control.command(wire.Monitor, map[string]any{"Name": "OPMonitor", "OPMonitor": map[string]any{"Action": "Stop", "Parameter": 5}})
	seen.await(t, "OPMonitor: json")
	title, err := wire.EncodeBitmap(wire.Bitmap{Width: 8, Height: 1, Rows: []byte{0x81}})
	if err != nil {
		t.Fatal(err)
	}
	control.send(wire.TitleBitmapSet, title[:len(title)-1])
	seen.await(t, "no 8×1 bitmap is 0 bytes")
	if _, _, _, drawn := camera.Title(); drawn {
		t.Fatal("a bitmap of no such size was kept")
	}

	sessionID := fmt.Sprintf("0x%08X", control.session)
	connect(t, camera).command(wire.MonitorClaim, map[string]any{"Name": "OPMonitor", "SessionID": 5, "OPMonitor": monitorOp("Claim", "Main")})
	seen.await(t, "claim's session")
	connect(t, camera).command(wire.MonitorClaim, map[string]any{"Name": "OPMonitor", "SessionID": "session", "OPMonitor": monitorOp("Claim", "Main")})
	seen.await(t, `claim for session "session"`)
	connect(t, camera).command(wire.MonitorClaim, map[string]any{"Name": "OPMonitor", "SessionID": sessionID, "OPMonitor": 5})
	seen.await(t, "OPMonitor: json")

	// One stream of one session is claimed once at a time.
	first := connect(t, camera)
	first.command(wire.MonitorClaim, map[string]any{"Name": "OPMonitor", "SessionID": sessionID, "OPMonitor": monitorOp("Claim", "Extra")})
	if _, err := wire.ReadPacket(first.conn); err != nil {
		t.Fatal(err)
	}
	connect(t, camera).command(wire.MonitorClaim, map[string]any{"Name": "OPMonitor", "SessionID": sessionID, "OPMonitor": monitorOp("Claim", "Extra")})
	seen.await(t, sessionID+"/0/Extra claimed while already claimed")
}
