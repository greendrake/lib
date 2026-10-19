// Package h265 describes an H.265 stream from a key frame's NAL units: its decoder
// configuration record and picture size. It derives in part from go2rtc's pkg/h265
// (github.com/AlexxIT/go2rtc), copyright (c) 2022 Alexey Khit, under the MIT licence in
// LICENSE beside it.
package h265

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/bluenviron/mediacommon/pkg/codecs/h264"
	mediah265 "github.com/bluenviron/mediacommon/pkg/codecs/h265"
)

// TypeOf is a NAL unit's type, which its header has.
func TypeOf(nalu []byte) mediah265.NALUType {
	return mediah265.NALUType(nalu[0] >> 1 & 0x3f)
}

// errNoParameterSets is a key frame without the VPS, SPS and PPS decoding starts from.
var errNoParameterSets = errors.New("h265: the key frame carries no VPS, SPS and PPS")

// ParameterSets are the types of the parameter sets, in the order a random access unit is
// preceded by them, and a decoder configuration record has them.
var ParameterSets = []mediah265.NALUType{mediah265.NALUType_VPS_NUT, mediah265.NALUType_SPS_NUT, mediah265.NALUType_PPS_NUT}

// Decoding is what decoding an H.265 stream starts from: the decoder configuration that
// describes it, and its picture size.
type Decoding struct {
	// The HEVCDecoderConfigurationRecord (ISO/IEC 14496-15 8.3.3.1), its NAL units 4-byte
	// length-prefixed.
	Config        []byte
	Width, Height int
}

// DecodingOf is the Decoding of a key frame, its NAL units, from the VPS, SPS and PPS it
// has to carry.
func DecodingOf(nalus [][]byte) (Decoding, error) {
	sets := map[mediah265.NALUType][]byte{}
	for _, nalu := range nalus {
		sets[TypeOf(nalu)] = nalu
	}
	for _, kind := range ParameterSets {
		if sets[kind] == nil {
			return Decoding{}, errNoParameterSets
		}
	}
	sps := sets[mediah265.NALUType_SPS_NUT]
	var parsed mediah265.SPS
	if err := parsed.Unmarshal(sps); err != nil {
		return Decoding{}, fmt.Errorf("h265: the SPS: %w", err)
	}
	// The SPS past its first byte, emulation prevention taken out, as the parser has read
	// it, 14 bytes at least: the general profile, tier and level are its bytes 2 to 13, as
	// the record has them.
	rbsp := h264.EmulationPreventionRemove(sps[1:])
	nested := byte(0)
	if parsed.TemporalIDNestingFlag {
		nested = 1
	}
	config := append([]byte{1}, rbsp[2:14]...)
	config = append(config,
		0xf0, 0x00, // reserved, min_spatial_segmentation_idc 0
		0xfc, // reserved, parallelismType 0
		0xfc|byte(parsed.ChromaFormatIdc),
		0xf8|byte(parsed.BitDepthLumaMinus8),
		0xf8|byte(parsed.BitDepthChromaMinus8),
		0, 0, // avgFrameRate
		// constantFrameRate 0, numTemporalLayers, temporalIdNested, lengthSizeMinusOne 3
		(parsed.MaxSubLayersMinus1+1)<<3|nested<<2|3,
		byte(len(ParameterSets)), // numOfArrays
	)
	for _, kind := range ParameterSets {
		// array_completeness 0, as the parameter sets come in band too; the type; one
		// NAL unit, behind its length.
		config = append(config, byte(kind), 0, 1)
		config = binary.BigEndian.AppendUint16(config, uint16(len(sets[kind])))
		config = append(config, sets[kind]...)
	}
	return Decoding{Config: config, Width: parsed.Width(), Height: parsed.Height()}, nil
}
