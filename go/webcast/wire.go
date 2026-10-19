package webcast

import (
	"encoding/binary"
	"fmt"
	"slices"
	"time"

	"greendrake.nz/go/camstream"
)

// frameHeaderSize is the size of a frame's wire form ahead of its data.
const frameHeaderSize = 1 + 1 + 1 + 8 + 8 + 4

// codecsOf are the codecs a frame of each kind can be in.
var codecsOf = map[camstream.Kind][]camstream.Codec{camstream.Video: {camstream.H265}, camstream.Audio: {camstream.PCMA, camstream.PCMU}}

// appendFrame appends f's wire form, the one a host relays frames to another in:
// [u8 kind][u8 codec][u8 keyframe][i64 PTS ns][i64 duration ns][u32 sample rate][data],
// big-endian.
func appendFrame(b []byte, f *camstream.Frame) []byte {
	keyframe := byte(0)
	if f.Keyframe {
		keyframe = 1
	}
	b = append(b, byte(f.Kind), byte(f.Codec), keyframe)
	b = binary.BigEndian.AppendUint64(b, uint64(f.PTS))
	b = binary.BigEndian.AppendUint64(b, uint64(f.Duration))
	b = binary.BigEndian.AppendUint32(b, uint32(f.SampleRate))
	return append(b, f.Data...)
}

// parseFrame reads a frame's wire form, its data b's own.
func parseFrame(b []byte) (*camstream.Frame, error) {
	if len(b) < frameHeaderSize {
		return nil, fmt.Errorf("webcast: %d bytes are no frame", len(b))
	}
	f := &camstream.Frame{
		Kind:       camstream.Kind(b[0]),
		Codec:      camstream.Codec(b[1]),
		PTS:        time.Duration(binary.BigEndian.Uint64(b[3:])),
		Duration:   time.Duration(binary.BigEndian.Uint64(b[11:])),
		SampleRate: int(binary.BigEndian.Uint32(b[19:])),
		Data:       b[frameHeaderSize:],
	}
	if !slices.Contains(codecsOf[f.Kind], f.Codec) || b[2] > 1 {
		return nil, fmt.Errorf("webcast: a frame of kind %d, codec %d, key frame %d", b[0], b[1], b[2])
	}
	f.Keyframe = b[2] == 1
	return f, nil
}
