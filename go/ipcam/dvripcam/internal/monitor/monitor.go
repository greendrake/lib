// Package monitor makes a DVRIP stream a camstream.Monitor: dvripcam's streams, and the
// recording genrecording writes for camstreamtest.
package monitor

import (
	"fmt"
	"time"

	"greendrake.nz/go/camstream"
	"greendrake.nz/go/dvrip"
)

var codecs = map[dvrip.Codec]camstream.Codec{
	dvrip.H265:  camstream.H265,
	dvrip.G711A: camstream.PCMA,
	dvrip.G711U: camstream.PCMU,
}

// Frames is where a monitor's frames come from: a camera's stream (a dvrip.Monitor), or
// a recording of one (a dvrip.Replay).
type Frames interface {
	Next() (dvrip.Frame, error)
}

// Monitor is a DVRIP stream as camstream takes it.
type Monitor struct {
	frames   Frames
	hasAudio bool
	shutDown func()
}

// New makes frames a monitor. hasAudio is whether the device puts audio in the stream,
// and shutDown disconnects it, unblocking a Next waiting on the device.
func New(frames Frames, hasAudio bool, shutDown func()) *Monitor {
	return &Monitor{frames: frames, hasAudio: hasAudio, shutDown: shutDown}
}

func (m *Monitor) Next() (*camstream.Frame, error) {
	f, err := m.frames.Next()
	if err != nil {
		return nil, err
	}
	codec, known := codecs[f.Codec]
	if !known {
		return nil, fmt.Errorf("unsupported codec %v", f.Codec)
	}
	out := &camstream.Frame{Codec: codec, PTS: f.PTS, Data: f.Data}
	if f.Kind == dvrip.Video {
		out.Kind = camstream.Video
		out.Keyframe = f.Keyframe
		out.Duration = time.Second / time.Duration(f.FPS)
	} else {
		out.Kind = camstream.Audio
		out.SampleRate = f.SampleRate
	}
	return out, nil
}

func (m *Monitor) HasAudio() bool {
	return m.hasAudio
}

func (m *Monitor) ShutDown() {
	m.shutDown()
}
