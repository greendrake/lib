package wire

import (
	"encoding/binary"
	"fmt"
)

// The type byte after the 00 00 01 every frame starts with.
const (
	FrameInfo    byte = 0xF9
	FrameAudio   byte = 0xFA
	FrameVideoI  byte = 0xFC
	FrameVideoP  byte = 0xFD
	FramePicture byte = 0xFE
)

// frameHeaders gives, by type byte, the size of the header in front of a frame's data,
// and where in it the data's length sits and how many bytes it takes.
var frameHeaders = map[byte]struct{ size, lengthAt, lengthWidth int }{
	FrameVideoI:  {16, 12, 4},
	FramePicture: {16, 12, 4},
	FrameVideoP:  {8, 4, 4},
	FrameAudio:   {8, 6, 2},
	FrameInfo:    {8, 6, 2},
}

// MaxFrameData bounds the data one frame may announce: several times the largest key
// frame a 4K stream produces, and far below what a header read out of place decodes to.
const MaxFrameData = 32 << 20

// RawFrame is one frame cut off the media stream: its type byte, header and data, and
// all of it as it came.
type RawFrame struct {
	Kind   byte
	Header []byte
	Data   []byte
	Bytes  []byte
}

// KeyframeRate is the frame rate a key frame's header states: the low five bits of
// its F byte.
func KeyframeRate(header []byte) int {
	return int(header[5] & 0x1F)
}

// Assembler cuts frames off the media stream. The payloads of a monitor's packets are
// one continuous byte stream: a packet may end part way into a frame and carry the
// start of the next, so frames are found by the lengths their headers state, never by
// packet boundaries.
type Assembler struct {
	buffer []byte
}

func (a *Assembler) Push(payload []byte) {
	a.buffer = append(a.buffer, payload...)
}

// Buffered is how many bytes have arrived past the last whole frame.
func (a *Assembler) Buffered() int {
	return len(a.buffer)
}

// Next cuts the next frame off the stream, or reports that it has not all arrived.
// The slices it returns stay valid: the buffer only ever grows past them.
func (a *Assembler) Next() (RawFrame, bool, error) {
	b := a.buffer
	if len(b) < 4 {
		return RawFrame{}, false, nil
	}
	if b[0] != 0 || b[1] != 0 || b[2] != 1 {
		return RawFrame{}, false, fmt.Errorf("dvrip: media stream out of place at % x", b[:4])
	}
	h, known := frameHeaders[b[3]]
	if !known {
		return RawFrame{}, false, fmt.Errorf("dvrip: unknown frame type %#02x", b[3])
	}
	if len(b) < h.size {
		return RawFrame{}, false, nil
	}
	var n int
	if h.lengthWidth == 4 {
		n = int(binary.LittleEndian.Uint32(b[h.lengthAt:]))
	} else {
		n = int(binary.LittleEndian.Uint16(b[h.lengthAt:]))
	}
	if n > MaxFrameData {
		return RawFrame{}, false, fmt.Errorf("dvrip: frame type %#02x announces %d bytes", b[3], n)
	}
	end := h.size + n
	if len(b) < end {
		return RawFrame{}, false, nil
	}
	a.buffer = b[end:]
	return RawFrame{Kind: b[3], Header: b[:h.size:h.size], Data: b[h.size:end:end], Bytes: b[:end:end]}, true, nil
}
