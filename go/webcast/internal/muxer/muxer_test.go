package muxer

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"

	mediah265 "github.com/bluenviron/mediacommon/pkg/codecs/h265"
	"greendrake.nz/go/camstream/camstreamtest"
	"greendrake.nz/go/camstream/h265"
)

// A camera's key frame, from a recording of its 800×448 H.265 stream.
func TestInitFromACamerasKeyFrame(t *testing.T) {
	key, err := camstreamtest.KeyFrame()
	if err != nil {
		t.Fatal(err)
	}
	init, err := (&Muxer{}).GetInit(key, 90000)
	if err != nil {
		t.Fatal(err)
	}
	entry := bytes.Index(init, []byte("hev1"))
	if entry < 0 {
		t.Fatal("no H.265 sample entry")
	}
	// The visual sample entry's width and height follow 24 bytes of other fields.
	if width, height := binary.BigEndian.Uint16(init[entry+28:]), binary.BigEndian.Uint16(init[entry+30:]); width != 800 || height != 448 {
		t.Fatalf("sample entry %dx%d", width, height)
	}
	// The parameter sets are the camera's.
	found := 0
	for _, nalu := range key {
		if !slices.Contains(h265.ParameterSets, h265.TypeOf(nalu)) {
			continue
		}
		found++
		if !bytes.Contains(init, nalu) {
			t.Fatalf("parameter set % x… is not in the configuration", nalu[:4])
		}
	}
	if found != 3 {
		t.Fatalf("the key frame has %d parameter sets", found)
	}
}

// A key frame without its parameter sets makes no initialization segment.
func TestNoInitWithoutParameterSets(t *testing.T) {
	if init, err := (&Muxer{}).GetInit([][]byte{camstreamtest.NALUnit(mediah265.NALUType_IDR_W_RADL)}, 90000); err == nil {
		t.Fatalf("an initialization segment of % x…", init[:8])
	}
}
