package h265

import (
	"bytes"
	"encoding/binary"
	"testing"

	mediah265 "github.com/bluenviron/mediacommon/pkg/codecs/h265"
	"greendrake.nz/go/camstream/camstreamtest"
)

// A camera's key frame describes its stream: the record, field by field, and the picture
// size.
func TestDecodingOfACamerasKeyFrame(t *testing.T) {
	key, err := camstreamtest.KeyFrame()
	if err != nil {
		t.Fatal(err)
	}
	d, err := DecodingOf(key)
	if err != nil {
		t.Fatal(err)
	}
	if d.Width != 800 || d.Height != 448 {
		t.Fatalf("%dx%d", d.Width, d.Height)
	}
	sets := map[mediah265.NALUType][]byte{}
	for _, nalu := range key {
		sets[TypeOf(nalu)] = nalu
	}
	var sps mediah265.SPS
	if err := sps.Unmarshal(sets[mediah265.NALUType_SPS_NUT]); err != nil {
		t.Fatal(err)
	}
	c := d.Config
	for _, field := range []struct {
		name      string
		got, want byte
	}{
		{"configurationVersion", c[0], 1},
		{"general_profile_idc", c[1] & 0x1f, 1},
		{"general_level_idc", c[12], sps.ProfileTierLevel.GeneralLevelIdc},
		{"min_spatial_segmentation_idc", c[13], 0xf0},
		{"parallelismType", c[15], 0xfc},
		{"chromaFormat", c[16], 0xfc | 1},
		{"bitDepthLumaMinus8", c[17], 0xf8},
		{"bitDepthChromaMinus8", c[18], 0xf8},
		{"lengthSizeMinusOne", c[21] & 3, 3},
		{"numOfArrays", c[22], 3},
	} {
		if field.got != field.want {
			t.Errorf("%s % x, want % x", field.name, field.got, field.want)
		}
	}
	at := 23
	for _, kind := range ParameterSets {
		// array_completeness 0, the type; one NAL unit, behind its length.
		size := int(binary.BigEndian.Uint16(c[at+3:]))
		if mediah265.NALUType(c[at]) != kind || binary.BigEndian.Uint16(c[at+1:]) != 1 || !bytes.Equal(c[at+5:at+5+size], sets[kind]) {
			t.Fatalf("array of % x", c[at:at+5])
		}
		at += 5 + size
	}
	if at != len(c) {
		t.Fatalf("%d bytes past the arrays", len(c)-at)
	}
}

// A 10-bit stream, of the Main 10 profile and the high tier, is described too.
func TestDecodingOfMain10(t *testing.T) {
	sps := []byte{
		0x42, 0x01, 0x01, 0x22, 0x20, 0x00, 0x00, 0x03,
		0x00, 0x90, 0x00, 0x00, 0x03, 0x00, 0x00, 0x03,
		0x00, 0x78, 0xa0, 0x03, 0xc0, 0x80, 0x10, 0xe4,
		0xd9, 0x66, 0x66, 0x92, 0x4c, 0xaf, 0x01, 0x01,
		0x00, 0x00, 0x03, 0x00, 0x64, 0x00, 0x00, 0x0b,
		0xb5, 0x08,
	}
	d, err := DecodingOf([][]byte{camstreamtest.NALUnit(mediah265.NALUType_VPS_NUT), sps, camstreamtest.NALUnit(mediah265.NALUType_PPS_NUT)})
	if err != nil {
		t.Fatal(err)
	}
	if d.Width != 1920 || d.Height != 1080 || d.Config[1] != 0x22 || d.Config[12] != 120 || d.Config[17] != 0xf8|2 {
		t.Fatalf("%dx%d, record % x…", d.Width, d.Height, d.Config[:23])
	}
}

func TestDecodingOfNothing(t *testing.T) {
	for _, nalus := range [][][]byte{
		{camstreamtest.NALUnit(mediah265.NALUType_IDR_W_RADL)},
		{camstreamtest.NALUnit(mediah265.NALUType_VPS_NUT), camstreamtest.NALUnit(mediah265.NALUType_SPS_NUT), camstreamtest.NALUnit(mediah265.NALUType_PPS_NUT)},
	} {
		if d, err := DecodingOf(nalus); err == nil {
			t.Errorf("%d NAL units described as %dx%d", len(nalus), d.Width, d.Height)
		}
	}
}
