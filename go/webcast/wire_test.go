package webcast

import (
	"reflect"
	"testing"
	"time"

	"greendrake.nz/go/camstream"
)

func TestFrameWire(t *testing.T) {
	for _, f := range []*camstream.Frame{
		{Kind: camstream.Video, Codec: camstream.H265, Keyframe: true, PTS: 10 * time.Second, Duration: 40 * time.Millisecond, Data: []byte{0, 0, 0, 1, 38, 1}},
		{Kind: camstream.Audio, Codec: camstream.PCMU, PTS: 10*time.Second + 20*time.Millisecond, SampleRate: 8000, Data: []byte{1, 2, 3}},
	} {
		if got, err := parseFrame(appendFrame(nil, f)); err != nil || !reflect.DeepEqual(got, f) {
			t.Errorf("%+v came back as %+v, %v", f, got, err)
		}
	}
	whole := appendFrame(nil, &camstream.Frame{Kind: camstream.Video, Codec: camstream.H265, Data: []byte{1}})
	for name, b := range map[string][]byte{
		"short":            whole[:frameHeaderSize-1],
		"no such kind":     append([]byte{3}, whole[1:]...),
		"a kind's codec":   append([]byte{byte(camstream.Audio)}, whole[1:]...),
		"no such codec":    append([]byte{byte(camstream.Video), 9}, whole[2:]...),
		"key frame byte 2": append([]byte{byte(camstream.Video), byte(camstream.H265), 2}, whole[3:]...),
	} {
		if f, err := parseFrame(b); err == nil {
			t.Errorf("%s: parsed %+v", name, f)
		}
	}
}
