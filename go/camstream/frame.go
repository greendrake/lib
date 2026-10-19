// Package camstream is a camera's stream, whatever brings it and wherever it goes: its
// frames, video or audio, with their codecs and timing; a connected stream (Monitor); and
// a stream fanned out to its subscribers while it has any (Source).
package camstream

import (
	"time"
)

// Kind is what a frame carries.
type Kind uint8

const (
	Video Kind = iota + 1
	Audio
)

// Codec is how a frame's data is encoded.
type Codec uint8

const (
	H265 Codec = iota + 1
	// G.711 A-law and µ-law.
	PCMA
	PCMU
)

// Frame is one video or audio frame of a camera stream.
type Frame struct {
	Kind  Kind
	Codec Codec
	// Video: whether decoding can start at this frame.
	Keyframe bool
	// When the frame is due on its stream's clock.
	PTS time.Duration
	// Video: how long the frame stays on screen.
	Duration time.Duration
	// Audio.
	SampleRate int
	// Video: an Annex-B access unit. Audio: the samples.
	Data []byte
}
