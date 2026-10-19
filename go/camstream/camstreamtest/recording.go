package camstreamtest

import (
	"bytes"
	_ "embed"
	"encoding/gob"
	"errors"
	"io"

	"github.com/bluenviron/mediacommon/pkg/codecs/h264"
	"greendrake.nz/go/camstream"
)

// recording is a camera's stream, in this package's own framing: its frames,
// gob-encoded as a []camstream.Frame. greendrake.nz/go/ipcam's dvripcam writes it, with
// WriteRecording, from the recording greendrake.nz/go/dvrip's dvriptest plays.
//
//go:embed recording.gob
var recording []byte

// Recording is a camera's stream as it sent it: 4.2 s of an XM530 camera's extra stream,
// 800×448 H.265 at 15 frames a second, its first video frame a key frame, with 8 kHz
// A-law audio.
func Recording() ([]*camstream.Frame, error) {
	var frames []*camstream.Frame
	err := gob.NewDecoder(bytes.NewReader(recording)).Decode(&frames)
	return frames, err
}

// WriteRecording writes frames in Recording's framing.
func WriteRecording(w io.Writer, frames []*camstream.Frame) error {
	return gob.NewEncoder(w).Encode(frames)
}

// KeyFrame is the NAL units of Recording's first key frame, its VPS, SPS and PPS among
// them.
func KeyFrame() ([][]byte, error) {
	frames, err := Recording()
	if err != nil {
		return nil, err
	}
	for _, f := range frames {
		if f.Kind == camstream.Video && f.Keyframe {
			var nalus h264.AnnexB
			err := nalus.Unmarshal(f.Data)
			return nalus, err
		}
	}
	return nil, errors.New("camstreamtest: the recording has no key frame")
}
