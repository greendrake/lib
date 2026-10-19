package dvrip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"greendrake.nz/go/dvrip/dvriptest"
	"greendrake.nz/go/dvrip/internal/wire"
)

// Frames as a device sends them, for streams built by tests.

func keyframeBytes(typ, fps, width, height byte, data []byte) []byte {
	b := []byte{0, 0, 1, wire.FrameVideoI, typ, fps, width, height, 0x21, 0x43, 0x65, 0x87}
	b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
	return append(b, data...)
}

func pframeBytes(data []byte) []byte {
	b := binary.LittleEndian.AppendUint32([]byte{0, 0, 1, wire.FrameVideoP}, uint32(len(data)))
	return append(b, data...)
}

func audioBytes(codec, rate byte, data []byte) []byte {
	b := binary.LittleEndian.AppendUint16([]byte{0, 0, 1, wire.FrameAudio, codec, rate}, uint16(len(data)))
	return append(b, data...)
}

func pictureBytes(data []byte) []byte {
	b := binary.LittleEndian.AppendUint32([]byte{0, 0, 1, wire.FramePicture, 0, 0, 0, 0, 0, 0, 0, 0}, uint32(len(data)))
	return append(b, data...)
}

func infoBytes(data []byte) []byte {
	b := binary.LittleEndian.AppendUint16([]byte{0, 0, 1, wire.FrameInfo, 1, 0}, uint16(len(data)))
	return append(b, data...)
}

// h265 is an H.265 key frame's T byte for a picture under 2048×2048.
const h265 byte = 0x03

// samples is 20 ms of 8 kHz G.711.
func samples(fill byte) []byte {
	return bytes.Repeat([]byte{fill}, 160)
}

// testStream is a stream as a camera opens one: a P-frame and audio from before the
// first key frame, video interleaved with audio, a picture and an info frame, and a key
// frame changing the frame rate.
func testStream() []byte {
	var b []byte
	for _, frame := range [][]byte{
		pframeBytes([]byte("orphan")),
		audioBytes(0x0E, 2, samples(0)),
		keyframeBytes(h265, 25, 80, 45, []byte("key one")),
		audioBytes(0x0E, 2, samples(1)),
		audioBytes(0x0E, 2, samples(2)),
		pframeBytes([]byte("p one")),
		pictureBytes([]byte("jpeg")),
		audioBytes(0x0E, 2, samples(3)),
		pframeBytes([]byte("p two")),
		infoBytes([]byte("GPS:;")),
		keyframeBytes(h265, 10, 80, 45, []byte("key two")),
		pframeBytes([]byte("p three")),
	} {
		b = append(b, frame...)
	}
	return b
}

// testStreamFrames is what testStream decodes to.
func testStreamFrames() []Frame {
	video := func(data string, keyframe bool, fps int, pts time.Duration) Frame {
		return Frame{Kind: Video, Codec: H265, Keyframe: keyframe, Width: 640, Height: 360, FPS: fps, PTS: pts, Data: []byte(data)}
	}
	audio := func(fill byte, pts time.Duration) Frame {
		return Frame{Kind: Audio, Codec: G711A, SampleRate: 8000, PTS: pts, Data: samples(fill)}
	}
	ms := time.Millisecond
	return []Frame{
		video("key one", true, 25, 0),
		audio(1, 0),
		audio(2, 20*ms),
		video("p one", false, 25, 40*ms),
		audio(3, 40*ms),
		video("p two", false, 25, 80*ms),
		// The frame rate changes where the next frame at the old rate was due.
		video("key two", true, 10, 120*ms),
		video("p three", false, 10, 220*ms),
	}
}

func sameFrames(t *testing.T, got, want []Frame) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d frames, want %d", len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Kind != w.Kind || g.Codec != w.Codec || g.Keyframe != w.Keyframe || g.Width != w.Width || g.Height != w.Height ||
			g.FPS != w.FPS || g.SampleRate != w.SampleRate || g.PTS != w.PTS || !bytes.Equal(g.Data, w.Data) {
			t.Fatalf("frame %d is\n%+v\nwant\n%+v", i, g, w)
		}
	}
}

// replay plays a recording to its end.
func replay(t *testing.T, r io.Reader) []Frame {
	t.Helper()
	p := NewReplay(r)
	var frames []Frame
	for {
		f, err := p.Next()
		if errors.Is(err, io.EOF) {
			return frames
		}
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, f)
	}
}

// decode replays a stream arriving in the pieces given.
func decode(t *testing.T, pieces ...[]byte) []Frame {
	t.Helper()
	readers := make([]io.Reader, len(pieces))
	for i, piece := range pieces {
		readers[i] = bytes.NewReader(piece)
	}
	return replay(t, io.MultiReader(readers...))
}

func TestStreamDecodes(t *testing.T) {
	sameFrames(t, decode(t, testStream()), testStreamFrames())
}

// Packets carry the stream with no regard for where frames start and end, so where it
// is cut must make no difference.
func TestStreamDecodesHoweverItIsCut(t *testing.T) {
	stream := testStream()
	want := testStreamFrames()
	for cut := 1; cut < len(stream); cut++ {
		sameFrames(t, decode(t, stream[:cut], stream[cut:]), want)
	}
	var bytewise [][]byte
	for i := range stream {
		bytewise = append(bytewise, stream[i:i+1])
	}
	sameFrames(t, decode(t, bytewise...), want)
}

// readRecording reads a stream an XM530 camera sent, as Monitor.Record saved it:
// 4.2 s of its 800×448 extra stream at 15 frames a second with a key frame every 2 s,
// and 8 kHz G.711 A-law audio in 20 ms frames.
func readRecording(t *testing.T) []byte {
	t.Helper()
	data, err := dvriptest.Recording()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRecordingReplays(t *testing.T) {
	var video, audio []Frame
	for _, f := range decode(t, readRecording(t)) {
		if f.Kind == Video {
			video = append(video, f)
		} else {
			audio = append(audio, f)
		}
	}
	if len(video) != 63 || len(audio) != 206 {
		t.Fatalf("%d video and %d audio frames", len(video), len(audio))
	}
	for i, f := range video {
		// Key frames open with the video parameter set.
		keyframe := i%30 == 0
		if f.Codec != H265 || f.Keyframe != keyframe || f.Width != 800 || f.Height != 448 || f.FPS != 15 ||
			f.PTS != time.Duration(i)*time.Second/15 || !bytes.HasPrefix(f.Data, []byte{0, 0, 0, 1}) ||
			keyframe && f.Data[4] != 0x40 {
			t.Fatalf("video frame %d: %v, key %v, %dx%d@%d at %v, data % x…", i, f.Codec, f.Keyframe, f.Width, f.Height, f.FPS, f.PTS, f.Data[:5])
		}
	}
	// Batched as it arrives, the audio is never pulled back.
	for i, f := range audio {
		if f.Codec != G711A || f.SampleRate != 8000 || len(f.Data) != 160 || f.PTS != audio[0].PTS+time.Duration(i)*20*time.Millisecond {
			t.Fatalf("audio frame %d: %v %d Hz, %d samples at %v", i, f.Codec, f.SampleRate, len(f.Data), f.PTS)
		}
	}
}

// Cut into pieces of every size up to 8 KB, the recording decodes as it does whole.
func TestRecordingDecodesHoweverItIsCut(t *testing.T) {
	stream := readRecording(t)
	var pieces [][]byte
	for size := 1; len(stream) > 0; size = size%(8<<10) + 1 {
		n := min(size, len(stream))
		pieces = append(pieces, stream[:n])
		stream = stream[n:]
	}
	sameFrames(t, decode(t, pieces...), decode(t, readRecording(t)))
}

func TestReplayOfACutRecording(t *testing.T) {
	stream := testStream()
	p := NewReplay(bytes.NewReader(stream[:len(stream)-1]))
	for range len(testStreamFrames()) - 1 {
		if _, err := p.Next(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Next(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %v, want the recording found cut", err)
	}
}

func TestReplayReadFails(t *testing.T) {
	failure := errors.New("unreadable")
	p := NewReplay(io.MultiReader(bytes.NewReader(testStream()[:30]), iotest.ErrReader(failure)))
	if _, err := p.Next(); !errors.Is(err, failure) {
		t.Fatalf("got %v", err)
	}
}

func TestKeyframeHeader(t *testing.T) {
	// 3840 = (0x0E0 + 1<<8) × 8 and 2160 = (0x00E + 1<<8) × 8: bits 4-5 and 6-7 of T
	// carry the size's high bits; the frame rate is F's low five bits.
	frames := decode(t, keyframeBytes(h265|0x10|0x40, 0xE0|15, 0xE0, 0x0E, []byte("4k")))
	f := frames[0]
	if f.Width != 3840 || f.Height != 2160 || f.FPS != 15 || f.Codec != H265 {
		t.Fatalf("decoded %+v", f)
	}
}

func TestAudioIsPulledBackToTheVideo(t *testing.T) {
	stream := keyframeBytes(h265, 25, 80, 45, []byte("key"))
	// Audio due up to the slack past the 40 ms frame is left alone, in 20 ms frames.
	inStep := int((40*time.Millisecond+audioSlack)/(20*time.Millisecond)) + 1
	for i := range inStep + 1 {
		stream = append(stream, audioBytes(0x0E, 2, samples(byte(i)))...)
	}
	frames := decode(t, stream)
	for i, f := range frames[1:] {
		want := time.Duration(i) * 20 * time.Millisecond
		if i == inStep {
			want = 0
		}
		if f.PTS != want {
			t.Fatalf("audio frame %d due at %v, want %v", i, f.PTS, want)
		}
	}
}

func TestAudioRateChangeAnchorsAgain(t *testing.T) {
	stream := append(keyframeBytes(h265, 25, 80, 45, []byte("key")), audioBytes(0x0E, 2, samples(0))...)
	stream = append(stream, audioBytes(0x0E, 4, bytes.Repeat([]byte{1}, 320))...)
	frames := decode(t, stream)
	if f := frames[2]; f.SampleRate != 16000 || f.PTS != 0 {
		t.Fatalf("decoded %+v", f)
	}
}

func TestBrokenStreams(t *testing.T) {
	tests := map[string]struct {
		stream []byte
		want   string
	}{
		"out of place":        {append([]byte{1}, testStream()...), "out of place"},
		"unknown frame type":  {[]byte{0, 0, 1, 0xF0, 0, 0, 0, 0}, "unknown frame type"},
		"implausible length":  {binary.LittleEndian.AppendUint32([]byte{0, 0, 1, wire.FrameVideoP}, wire.MaxFrameData+1), "announces"},
		"unknown video codec": {keyframeBytes(0x07, 25, 80, 45, nil), "video codec"},
		"H.264":               {keyframeBytes(0x02, 25, 80, 45, nil), "video codec 0x02"},
		"no frame rate":       {keyframeBytes(h265, 0x20, 80, 45, nil), "no frame rate"},
		"unknown audio codec": {audioBytes(0x01, 2, nil), "audio codec"},
		"no sample rate":      {audioBytes(0x0E, 0, nil), "sample rate"},
		"unknown sample rate": {audioBytes(0x0E, 10, nil), "sample rate"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var (
				a  wire.Assembler
				tl timeline
			)
			a.Push(test.stream)
			raw, whole, err := a.Next()
			if err == nil && whole {
				_, _, err = tl.frame(raw)
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want an error about %q", err, test.want)
			}
		})
	}
}

func TestCodecNames(t *testing.T) {
	if H265.String() != "H265" || G711A.String() != "G711A" || Codec(99).String() != "Codec(99)" {
		t.Fatal("codec names")
	}
}
