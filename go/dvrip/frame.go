package dvrip

import (
	"errors"
	"fmt"
	"io"
	"time"

	"greendrake.nz/go/dvrip/internal/wire"
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
	MPEG4 Codec = iota + 1
	H265
	G711A
	G711U
)

var codecNames = map[Codec]string{
	MPEG4: "MPEG4",
	H265:  "H265",
	G711A: "G711A",
	G711U: "G711U",
}

func (c Codec) String() string {
	if name, ok := codecNames[c]; ok {
		return name
	}
	return fmt.Sprintf("Codec(%d)", uint8(c))
}

// Frame is one video or audio frame of a monitored stream.
type Frame struct {
	Kind  Kind
	Codec Codec
	// Video: whether the frame starts a group of pictures. Key frames carry the
	// parameter sets in band.
	Keyframe bool
	// Video: the picture size and the frame rate the encoder runs at, as the latest key
	// frame states them.
	Width, Height, FPS int
	// Audio.
	SampleRate int
	// When the frame is due on the stream's clock, which starts at 0 with the first key
	// frame. The device stamps no frame finer than a second, so video advances by one
	// frame period per frame and audio by its sample count, kept within reach of the
	// video it arrives with.
	PTS time.Duration
	// Video: one Annex-B access unit. Audio: the samples, one byte each.
	Data []byte
}

// The low four bits of a header's T byte, by frame type. Video and audio codes are
// the vendor's frame-format document's, plus H.265 and G.711 µ-law as devices send them.
// H.264 (0x2) is left out: a stream in it is refused as an unknown codec.
var (
	videoCodecs = map[byte]Codec{0x1: MPEG4, 0x3: H265}
	audioCodecs = map[byte]Codec{0xE: G711A, 0xA: G711U}
)

// sampleRates is indexed by an audio header's S byte, less one.
var sampleRates = []int{4000, 8000, 11025, 16000, 20000, 22050, 32000, 44100, 48000}

// frameStream turns a media byte stream, coming in pieces from read, into the frames
// of one clock.
type frameStream struct {
	read     func() ([]byte, error)
	frames   wire.Assembler
	timeline timeline
	// Receives every frame as it was cut, before anything is made of it.
	record io.Writer
}

func (s *frameStream) next() (Frame, error) {
	for {
		raw, whole, err := s.frames.Next()
		if err != nil {
			return Frame{}, err
		}
		if !whole {
			piece, err := s.read()
			if errors.Is(err, io.EOF) && s.frames.Buffered() > 0 {
				err = io.ErrUnexpectedEOF
			}
			if err != nil {
				return Frame{}, err
			}
			s.frames.Push(piece)
			continue
		}
		if s.record != nil {
			if _, err := s.record.Write(raw.Bytes); err != nil {
				return Frame{}, err
			}
		}
		f, part, err := s.timeline.frame(raw)
		if err != nil {
			return Frame{}, err
		}
		if part {
			return f, nil
		}
	}
}

// replayPiece is how much of a recording a replay reads at a time.
const replayPiece = 16 << 10

// Replay plays back a stream Monitor.Record saved.
type Replay struct {
	stream frameStream
}

// NewReplay plays back the recording r reads.
func NewReplay(r io.Reader) *Replay {
	buffer := make([]byte, replayPiece)
	return &Replay{stream: frameStream{read: func() ([]byte, error) {
		n, err := r.Read(buffer)
		if n > 0 {
			return buffer[:n], nil
		}
		return nil, err
	}}}
}

// Next returns the recording's next frame, as Monitor.Next did; io.EOF once it is all
// played, io.ErrUnexpectedEOF if it ends part way into a frame.
func (p *Replay) Next() (Frame, error) {
	return p.stream.next()
}

// audioSlack is how far audio may wander outside the video frame it arrives with
// before it is pulled back onto the video's clock. The cameras send audio in batches of
// three or four 40 ms frames, which by their samples land up to 175 ms past the video
// they arrive with; their audio clock does not drift from the video's. So the slack
// spans the batching with room to spare, and only lost audio exceeds it.
const audioSlack = 250 * time.Millisecond

// timeline turns raw frames into a stream on one clock.
type timeline struct {
	started bool
	// The latest key frame's properties, which the frames after it share.
	codec              Codec
	width, height, fps int
	// The next video frame is due at videoBase plus videoFrames periods of the current rate.
	videoBase   time.Duration
	videoFrames int64
	lastVideo   time.Duration
	// The next audio frame is due at audioBase plus audioSamples at audioRate.
	audioBase    time.Duration
	audioSamples int64
	audioRate    int
}

// frame stamps a raw frame. False means the frame is not part of the stream: anything
// before the first key frame, which nothing could decode, and pictures and info frames.
func (t *timeline) frame(raw wire.RawFrame) (Frame, bool, error) {
	switch raw.Kind {
	case wire.FrameVideoI:
		return t.keyframe(raw)
	case wire.FrameVideoP:
		if !t.started {
			return Frame{}, false, nil
		}
		return t.video(raw.Data, false), true, nil
	case wire.FrameAudio:
		codec, known := audioCodecs[raw.Header[4]]
		if !known {
			return Frame{}, false, fmt.Errorf("dvrip: unknown audio codec %#02x", raw.Header[4])
		}
		rate := int(raw.Header[5])
		if rate < 1 || rate > len(sampleRates) {
			return Frame{}, false, fmt.Errorf("dvrip: unknown audio sample rate code %d", rate)
		}
		if !t.started {
			return Frame{}, false, nil
		}
		return t.audio(codec, sampleRates[rate-1], raw.Data), true, nil
	}
	return Frame{}, false, nil
}

// keyframe decodes a key frame's header: T carries the codec in bits 0-3 and the ninth
// and tenth bits of width and height in bits 4-5 and 6-7; F carries the frame rate in
// bits 0-4; W and H are the low bits of the picture size in units of 8 pixels.
func (t *timeline) keyframe(raw wire.RawFrame) (Frame, bool, error) {
	typ := raw.Header[4]
	codec, known := videoCodecs[typ&0x0F]
	if !known {
		return Frame{}, false, fmt.Errorf("dvrip: unknown video codec %#02x", typ&0x0F)
	}
	fps := wire.KeyframeRate(raw.Header)
	if fps == 0 {
		return Frame{}, false, fmt.Errorf("dvrip: key frame states no frame rate")
	}
	if !t.started {
		t.started = true
	} else if fps != t.fps {
		t.videoBase = t.nextVideo()
		t.videoFrames = 0
	}
	t.codec = codec
	t.fps = fps
	t.width = (int(raw.Header[6]) | int(typ>>4&3)<<8) * 8
	t.height = (int(raw.Header[7]) | int(typ>>6&3)<<8) * 8
	return t.video(raw.Data, true), true, nil
}

func (t *timeline) nextVideo() time.Duration {
	return t.videoBase + time.Duration(t.videoFrames)*time.Second/time.Duration(t.fps)
}

func (t *timeline) video(data []byte, keyframe bool) Frame {
	pts := t.nextVideo()
	t.videoFrames++
	t.lastVideo = pts
	return Frame{
		Kind:     Video,
		Codec:    t.codec,
		Keyframe: keyframe,
		Width:    t.width,
		Height:   t.height,
		FPS:      t.fps,
		PTS:      pts,
		Data:     data,
	}
}

// audio stamps G.711, one byte a sample, by its sample count since the clock was last
// anchored to the video, and anchors it again when the count has drifted outside the
// video frame it arrives with.
func (t *timeline) audio(codec Codec, rate int, data []byte) Frame {
	pts := t.lastVideo
	inStep := rate == t.audioRate
	if inStep {
		pts = t.audioBase + time.Duration(t.audioSamples)*time.Second/time.Duration(rate)
		period := time.Second / time.Duration(t.fps)
		inStep = pts >= t.lastVideo-audioSlack && pts <= t.lastVideo+period+audioSlack
	}
	if !inStep {
		pts = t.lastVideo
		t.audioBase = pts
		t.audioSamples = 0
		t.audioRate = rate
	}
	t.audioSamples += int64(len(data))
	return Frame{
		Kind:       Audio,
		Codec:      codec,
		SampleRate: rate,
		PTS:        pts,
		Data:       data,
	}
}
