package dvrip

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"greendrake.nz/go/dvrip/dvriptest"
	"greendrake.nz/go/dvrip/internal/wire"
)

// streaming is a camera whose monitors carry testStream, cut into packets of odd sizes.
func streaming(options dvriptest.Options) dvriptest.Options {
	options.Media = testStream()
	options.Split = func(b []byte) [][]byte {
		var chunks [][]byte
		for size := 7; len(b) > 0; size += 13 {
			n := min(size, len(b))
			chunks = append(chunks, b[:n])
			b = b[n:]
		}
		return chunks
	}
	return options
}

func nextFrames(t *testing.T, m *Monitor, n int) []Frame {
	t.Helper()
	var frames []Frame
	for range n {
		f, err := m.Next()
		if err != nil {
			t.Fatalf("after %d frames: %v", len(frames), err)
		}
		frames = append(frames, f)
	}
	return frames
}

func TestMonitorStreams(t *testing.T) {
	camera := dvriptest.New(t, "techage", streaming(dvriptest.Options{}))
	s := dial(t, camera)
	m, err := s.Monitor(context.Background(), 0, Extra)
	if err != nil {
		t.Fatal(err)
	}
	want := testStreamFrames()
	sameFrames(t, nextFrames(t, m, len(want)), want)

	// Claimed on the media connection, then started on the control connection.
	monitors := camera.Commands(opMonitor)
	if len(monitors) != 2 || wire.Code(monitors[0].Code) != wire.MonitorClaim || wire.Code(monitors[1].Code) != wire.Monitor {
		t.Fatalf("monitor commands %+v", monitors)
	}
	for i, action := range []string{"Claim", "Start"} {
		var op monitorAction
		payload(t, monitors[i], &op)
		want := monitorAction{Action: action, Parameter: monitorParameter{Channel: 0, CombinMode: "NONE", StreamType: Extra, TransMode: "TCP"}}
		if op != want {
			t.Fatalf("%s sent as %+v", action, op)
		}
	}
	var session string
	if err := json.Unmarshal(monitors[0].Body["SessionID"], &session); err != nil || session != "0x00000057" {
		t.Fatalf("claimed for session %q, %v", session, err)
	}
}

// One session runs both streams at once, each on its own media connection.
func TestMonitorsOfBothStreams(t *testing.T) {
	s := dial(t, dvriptest.New(t, "techage", dvriptest.Options{}))
	for _, stream := range []StreamType{Main, Extra} {
		m, err := s.Monitor(context.Background(), 0, stream)
		if err != nil {
			t.Fatal(err)
		}
		if f := nextFrames(t, m, 1)[0]; !f.Keyframe || f.Codec != H265 {
			t.Fatalf("%s opened with %+v", stream, f.Kind)
		}
		defer m.Close(context.Background())
	}
}

func TestMonitorRecordsTheStream(t *testing.T) {
	s := dial(t, dvriptest.New(t, "techage", streaming(dvriptest.Options{})))
	m, err := s.Monitor(context.Background(), 0, Main)
	if err != nil {
		t.Fatal(err)
	}
	var recording bytes.Buffer
	m.Record(&recording)
	frames := nextFrames(t, m, len(testStreamFrames()))
	if !bytes.Equal(recording.Bytes(), testStream()) {
		t.Fatal("the recording is not the stream")
	}
	sameFrames(t, replay(t, &recording), frames)
}

func TestMonitorRefused(t *testing.T) {
	for _, action := range []string{"Claim", "Start"} {
		t.Run(action, func(t *testing.T) {
			s := dial(t, dvriptest.New(t, "techage", streaming(dvriptest.Options{Refuse: map[string]int{"OPMonitor." + action: 103}})))
			var retErr *RetError
			if _, err := s.Monitor(context.Background(), 0, Main); !errors.As(err, &retErr) || retErr.Ret != 103 {
				t.Fatalf("got %v, want 103", err)
			}
			s.mediaMutex.Lock()
			open := len(s.media)
			s.mediaMutex.Unlock()
			if open != 0 {
				t.Fatalf("%d media connections left open", open)
			}
			if err := s.Err(); err != nil {
				t.Fatalf("a refused monitor ended the session: %v", err)
			}
		})
	}
}

func TestMonitorCloseStopsTheStream(t *testing.T) {
	camera := dvriptest.New(t, "techage", streaming(dvriptest.Options{}))
	s := dial(t, camera)
	m, err := s.Monitor(context.Background(), 0, Main)
	if err != nil {
		t.Fatal(err)
	}
	nextFrames(t, m, 1)
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	monitors := camera.Commands(opMonitor)
	var op monitorAction
	payload(t, monitors[len(monitors)-1], &op)
	if op.Action != "Stop" {
		t.Fatalf("closed with %+v", op)
	}
	if _, err := m.Next(); err == nil {
		t.Fatal("a closed monitor delivered")
	}
	if err := s.Err(); err != nil {
		t.Fatalf("stopping a stream ended the session: %v", err)
	}
	// The session goes on to serve the stream again.
	another, err := s.Monitor(context.Background(), 0, Main)
	if err != nil {
		t.Fatal(err)
	}
	nextFrames(t, another, 1)
}

func TestMonitorStalls(t *testing.T) {
	s := dial(t, dvriptest.New(t, "techage", streaming(dvriptest.Options{})), WithStallLimit(100*time.Millisecond))
	m, err := s.Monitor(context.Background(), 0, Main)
	if err != nil {
		t.Fatal(err)
	}
	nextFrames(t, m, len(testStreamFrames()))
	if _, err := m.Next(); err == nil || !strings.Contains(err.Error(), "media silent") {
		t.Fatalf("got %v, want the stream found silent", err)
	}
}

func TestMonitorEndsWithItsSession(t *testing.T) {
	s := dial(t, dvriptest.New(t, "techage", streaming(dvriptest.Options{})))
	m, err := s.Monitor(context.Background(), 0, Main)
	if err != nil {
		t.Fatal(err)
	}
	nextFrames(t, m, len(testStreamFrames()))
	go s.Close()
	if _, err := m.Next(); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v, want the session's end", err)
	}
}

func TestMonitorRefusesForeignMessages(t *testing.T) {
	s := dial(t, dvriptest.New(t, "techage", streaming(dvriptest.Options{MediaCode: uint16(wire.Monitor + 1)})))
	m, err := s.Monitor(context.Background(), 0, Main)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Next(); err == nil || !strings.Contains(err.Error(), "message 1411") {
		t.Fatalf("got %v", err)
	}
}

func TestMonitorRefusesABrokenStream(t *testing.T) {
	tests := map[string]struct {
		stream []byte
		want   string
	}{
		"unknown frame":   {[]byte{0, 0, 1, 0xF0, 0, 0, 0, 0}, "unknown frame type"},
		"undecodable key": {keyframeBytes(0x07, 25, 80, 45, nil), "video codec"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			s := dial(t, dvriptest.New(t, "techage", dvriptest.Options{Media: test.stream}))
			m, err := s.Monitor(context.Background(), 0, Main)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Next(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want an error about %q", err, test.want)
			}
		})
	}
}

func TestMonitorClaimNeverGranted(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{Silent: map[string]bool{"OPMonitor.Claim": true}})
	t.Run("cancelled", func(t *testing.T) {
		s := dial(t, camera)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := s.Monitor(ctx, 0, Main); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v, want the deadline", err)
		}
	})
	t.Run("stalled", func(t *testing.T) {
		s := dial(t, camera, WithStallLimit(50*time.Millisecond))
		if _, err := s.Monitor(context.Background(), 0, Main); err == nil || !strings.Contains(err.Error(), "media silent") {
			t.Fatalf("got %v, want the claim found silent", err)
		}
	})
}

func TestMonitorOfAnUnreachableDevice(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{})
	s := dial(t, camera)
	camera.Close()
	if _, err := s.Monitor(context.Background(), 0, Main); err == nil {
		t.Fatal("a monitor needs a connection of its own")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("disk full")
}

func TestMonitorRecordingFails(t *testing.T) {
	s := dial(t, dvriptest.New(t, "techage", streaming(dvriptest.Options{})))
	m, err := s.Monitor(context.Background(), 0, Main)
	if err != nil {
		t.Fatal(err)
	}
	m.Record(failingWriter{})
	if _, err := m.Next(); err == nil || err.Error() != "disk full" {
		t.Fatalf("got %v", err)
	}
}

func TestMonitorGivesUpWhenCancelled(t *testing.T) {
	s := dial(t, dvriptest.New(t, "techage", dvriptest.Options{Silent: map[string]bool{"OPMonitor.Start": true}}))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.Monitor(ctx, 0, Main); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want the deadline", err)
	}
}
