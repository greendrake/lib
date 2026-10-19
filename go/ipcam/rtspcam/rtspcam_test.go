package rtspcam

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/pkg/codecs/h264"
	mediah265 "github.com/bluenviron/mediacommon/pkg/codecs/h265"
	"greendrake.nz/go/camstream"
	"greendrake.nz/go/camstream/h265"
	"greendrake.nz/go/ipcam"
	"greendrake.nz/go/ipcam/rtspcam/internal/recording"
	"greendrake.nz/go/rtsp"
	"greendrake.nz/go/rtsp/rtsptest"
)

const (
	user     = "admin"
	password = "pass"
)

// recorded is a recorded camera's stream, as its access units.
func recorded(t *testing.T) []rtsptest.Unit {
	t.Helper()
	units, err := recording.Units()
	if err != nil {
		t.Fatal(err)
	}
	return units
}

// nalusOf is an Annex-B access unit's NAL units.
func nalusOf(t *testing.T, annexB []byte) [][]byte {
	t.Helper()
	var nalus h264.AnnexB
	if err := nalus.Unmarshal(annexB); err != nil {
		t.Fatal(err)
	}
	return nalus
}

func serve(t *testing.T, streams map[string]rtsptest.Stream) *rtsptest.Camera {
	return rtsptest.Serve(t, rtsptest.Options{User: user, Password: password, Streams: streams})
}

func dial(t *testing.T, driver Driver, address, password string) (*login, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cam, err := driver.Dial(ctx, address, user, password)
	if err != nil {
		return nil, err
	}
	t.Cleanup(cam.Close)
	return cam.(*login), nil
}

func TestDialDescribesBothStreams(t *testing.T) {
	c := serve(t, map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", nil), "/1": rtsptest.Video("H264", nil)})
	l, err := dial(t, BITVISION, c.Address(), password)
	if err != nil {
		t.Fatal(err)
	}
	if streams := l.Streams(); streams[ipcam.Main] != (ipcam.StreamFormat{Codec: ipcam.H265}) || streams[ipcam.Extra] != (ipcam.StreamFormat{Codec: ipcam.H264}) {
		t.Fatalf("streams %+v", streams)
	}
	// A codec the core has no name of goes by its encoding's.
	c = serve(t, map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", nil), "/1": rtsptest.Video("JPEG", nil)})
	if l, err = dial(t, BITVISION, c.Address(), password); err != nil || l.Streams()[ipcam.Extra].Codec != "JPEG" {
		t.Fatalf("%+v, %v", l, err)
	}
	if _, settings := ipcam.Driver(BITVISION).(ipcam.SettingsDriver); settings {
		t.Fatal("RTSP cameras have settings")
	}
}

// A driver's cameras serve their streams where its paths say.
func TestPaths(t *testing.T) {
	c := serve(t, map[string]rtsptest.Stream{"/main": rtsptest.Video("H265", nil), "/sub": rtsptest.Video("H264", nil)})
	l, err := dial(t, Driver{Paths: map[ipcam.StreamKey]string{ipcam.Main: "/main", ipcam.Extra: "/sub"}, KeepAlive: time.Minute}, c.Address(), password)
	if err != nil {
		t.Fatal(err)
	}
	if streams := l.Streams(); streams[ipcam.Main].Codec != ipcam.H265 || streams[ipcam.Extra].Codec != ipcam.H264 {
		t.Fatalf("streams %+v", streams)
	}
}

func TestDialFails(t *testing.T) {
	c := serve(t, map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", nil)})
	silent := serve(t, map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", nil), "/1": {Type: "audio", Encoding: "PCMA", ClockRate: 8000}})
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	for _, test := range []struct {
		what, address, password string
		want                    error
	}{
		{"a refused password", c.Address(), "wrong", ipcam.ErrCredentials},
		{"no camera", closed.Addr().String(), password, ipcam.ErrUnreachable},
		{"a stream not served", c.Address(), password, ipcam.ErrUnrecognised},
		{"a stream of no video", silent.Address(), password, ipcam.ErrUnrecognised},
	} {
		if _, err := dial(t, BITVISION, test.address, test.password); !errors.Is(err, test.want) {
			t.Errorf("%s: %v", test.what, err)
		}
	}
}

// next is the monitor's next frame, which must be a video frame of a duration.
func next(t *testing.T, m *monitor) *camstream.Frame {
	t.Helper()
	f, err := m.Next()
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != camstream.Video || f.Codec != camstream.H265 || f.Duration <= 0 {
		t.Fatalf("frame %+v", f)
	}
	return f
}

// parameterised says whether f opens with the parameter sets: VPS, SPS and PPS.
func parameterised(t *testing.T, f *camstream.Frame) bool {
	t.Helper()
	nalus := nalusOf(t, f.Data)
	for i, want := range h265.ParameterSets {
		if len(nalus) <= i || h265.TypeOf(nalus[i]) != want {
			return false
		}
	}
	return true
}

// A stream plays its video over the driver's transport, every random access unit with
// the parameter sets ahead of it, and shutting it down ends it.
func TestStreamPlays(t *testing.T) {
	units := recorded(t)
	for name, transport := range map[string]rtsp.Transport{"UDP": rtsp.UDP, "TCP": rtsp.TCP} {
		c := serve(t, map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", nil), "/1": rtsptest.Video("H265", units)})
		driver := BITVISION
		driver.Transport = transport
		l, err := dial(t, driver, c.Address(), password)
		if err != nil {
			t.Fatal(err)
		}
		opened, err := l.OpenStream(context.Background(), ipcam.Extra)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		m := opened.(*monitor)
		keyframes := 0
		for range 20 {
			if f := next(t, m); f.Keyframe {
				keyframes++
				if !parameterised(t, f) {
					t.Fatalf("%s: a key frame of % x…", name, f.Data[:min(len(f.Data), 16)])
				}
			}
		}
		if keyframes == 0 || m.HasAudio() {
			t.Fatalf("%s: %d key frames, audio %v", name, keyframes, m.HasAudio())
		}
		m.ShutDown()
		if _, err := m.Next(); err == nil {
			t.Fatalf("%s: a shut stream went on", name)
		}
	}
}

// The parameter sets a stream's description carries go ahead of its random access
// units where the stream itself sends none.
func TestParameterSetsOfTheDescription(t *testing.T) {
	units := recorded(t)
	params := map[mediah265.NALUType][]byte{}
	for i, unit := range units {
		var rest [][]byte
		for _, nalu := range unit.NALUs {
			if kind := h265.TypeOf(nalu); slices.Contains(h265.ParameterSets, kind) {
				params[kind] = nalu
				continue
			}
			rest = append(rest, nalu)
		}
		units[i].NALUs = rest
	}
	sprop := func(kind mediah265.NALUType) string { return base64.StdEncoding.EncodeToString(params[kind]) }
	stream := rtsptest.Video("H265", units)
	stream.Params = "sprop-vps=" + sprop(mediah265.NALUType_VPS_NUT) + ";sprop-sps=" + sprop(mediah265.NALUType_SPS_NUT) + ";sprop-pps=" + sprop(mediah265.NALUType_PPS_NUT)
	c := serve(t, map[string]rtsptest.Stream{"/0": stream})
	m, err := play(context.Background(), c.Address(), "/0", user, password, rtsp.UDP)
	if err != nil {
		t.Fatal(err)
	}
	defer m.ShutDown()
	for f := next(t, m); ; f = next(t, m) {
		if f.Keyframe {
			if !parameterised(t, f) {
				t.Fatalf("a key frame of % x…", f.Data[:min(len(f.Data), 16)])
			}
			return
		}
	}
}

// A stream whose timestamps stand still is timed by the frame rate its description gives,
// and fails where it gives none.
func TestStillTimestamps(t *testing.T) {
	timed := rtsptest.Video("H265", recorded(t))
	timed.Still, timed.FrameRate = true, 10
	untimed := timed
	untimed.FrameRate = 0
	c := serve(t, map[string]rtsptest.Stream{"/0": timed, "/1": untimed})
	m, err := play(context.Background(), c.Address(), "/0", user, password, rtsp.UDP)
	if err != nil {
		t.Fatal(err)
	}
	defer m.ShutDown()
	for range 3 {
		if f := next(t, m); f.Duration != 100*time.Millisecond {
			t.Fatalf("a frame of %v", f.Duration)
		}
	}
	m, err = play(context.Background(), c.Address(), "/1", user, password, rtsp.UDP)
	if err != nil {
		t.Fatal(err)
	}
	defer m.ShutDown()
	if _, err := m.Next(); err == nil {
		t.Fatal("a stream with no time played")
	}
}

// An open is bounded by its context, and fails on a stream of no H.265.
func TestOpenFails(t *testing.T) {
	c := serve(t, map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", nil), "/1": rtsptest.Video("H264", nil)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := play(ctx, c.Address(), "/0", user, password, rtsp.UDP); err == nil {
		t.Fatal("an open went on past its context")
	}
	if _, err := play(context.Background(), c.Address(), "/1", user, password, rtsp.UDP); err == nil {
		t.Fatal("an H.264 stream was opened")
	}
}

// A login is probed every keep-alive interval while it lasts, and ends with a probe the
// camera leaves unanswered.
func TestLoginEndsWithAFailedProbe(t *testing.T) {
	c := serve(t, map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", nil), "/1": rtsptest.Video("H265", nil)})
	driver := BITVISION
	driver.KeepAlive = 50 * time.Millisecond
	l, err := dial(t, driver, c.Address(), password)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); c.Requests("OPTIONS") < 3; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the login was not probed")
		}
	}
	select {
	case <-l.Done():
		t.Fatalf("a login the camera answers ended: %v", l.Err())
	default:
	}
	c.Hold()
	select {
	case <-l.Done():
		if !errors.Is(l.Err(), rtsp.ErrNoAnswer) {
			t.Fatalf("ended for %v", l.Err())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a login the camera left unanswered went on")
	}
}

// The login ends with its connection.
func TestLoginEndsWithTheCamera(t *testing.T) {
	c := serve(t, map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", nil), "/1": rtsptest.Video("H265", nil)})
	l, err := dial(t, BITVISION, c.Address(), password)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	select {
	case <-l.Done():
		if l.Err() == nil {
			t.Fatal("ended for no reason")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the login outlived its connection")
	}
}
