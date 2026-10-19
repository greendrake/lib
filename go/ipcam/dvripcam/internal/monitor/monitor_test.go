package monitor

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"greendrake.nz/go/camstream"
	"greendrake.nz/go/dvrip"
	"greendrake.nz/go/dvrip/dvriptest"
)

// A camera's recorded stream comes out as camstream frames: 800×448 H.265 at 15 frames a
// second, and 8 kHz A-law audio.
func TestRecordedStream(t *testing.T) {
	recording, err := dvriptest.Recording()
	if err != nil {
		t.Fatal(err)
	}
	shut := false
	m := New(dvrip.NewReplay(bytes.NewReader(recording)), true, func() { shut = true })
	var video, audio int
	for {
		f, err := m.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case f.Kind == camstream.Video && f.Codec == camstream.H265 && f.Duration == time.Second/15 && (video > 0 || f.Keyframe):
			video++
		case f.Kind == camstream.Audio && f.Codec == camstream.PCMA && f.SampleRate == 8000 && len(f.Data) == 160:
			audio++
		default:
			t.Fatalf("frame %+v", f.Kind)
		}
	}
	if video != 63 || audio != 206 || !m.HasAudio() {
		t.Fatalf("%d video and %d audio frames", video, audio)
	}
	m.ShutDown()
	if !shut {
		t.Fatal("shutting down did not disconnect")
	}
}

type frames []dvrip.Frame

func (f *frames) Next() (dvrip.Frame, error) {
	if len(*f) == 0 {
		return dvrip.Frame{}, io.EOF
	}
	next := (*f)[0]
	*f = (*f)[1:]
	return next, nil
}

func TestUnsupportedCodec(t *testing.T) {
	m := New(&frames{{Kind: dvrip.Video, Codec: dvrip.MPEG4, FPS: 25}}, false, func() {})
	if _, err := m.Next(); err == nil {
		t.Fatal("MPEG-4 went through")
	}
}
