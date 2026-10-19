// Package muxer muxes an H.265 stream into fragmented MP4: the initialisation segment from
// a key frame, then a fragment per frame. It derives from go2rtc's pkg/mp4, pkg/iso and
// pkg/core (github.com/AlexxIT/go2rtc), copyright (c) 2022 Alexey Khit, under the MIT
// licence in LICENSE beside it.
package muxer

import (
	"greendrake.nz/go/camstream/h265"
)

type Muxer struct {
	index uint32
}

// GetInit is the initialization segment of an H.265 stream starting at a key frame, its NAL
// units: a single track, described by h265.DecodingOf, on clockRate.
func (m *Muxer) GetInit(nalus [][]byte, clockRate uint32) ([]byte, error) {
	decoding, err := h265.DecodingOf(nalus)
	if err != nil {
		return nil, err
	}
	mv := NewMovie(1024)
	mv.WriteFileType()

	mv.StartAtom(Moov)
	mv.WriteMovieHeader()
	mv.WriteVideoTrack(1, codecH265, clockRate, uint16(decoding.Width), uint16(decoding.Height), decoding.Config)

	mv.StartAtom(MoovMvex)
	mv.WriteTrackExtend(1)
	mv.EndAtom() // MVEX

	mv.EndAtom() // MOOV

	return mv.Bytes(), nil
}

// GetPayload makes a media segment of one sample: decoded at dts and lasting duration,
// both in the track's clock rate.
func (m *Muxer) GetPayload(trackID byte, payload []byte, keyframe bool, dts uint64, duration uint32) []byte {
	m.index++
	flags := uint32(SampleVideoNonIFrame)
	if keyframe {
		flags = SampleVideoIFrame
	}
	mv := NewMovie(1024 + len(payload))
	mv.WriteMovieFragment(m.index, uint32(trackID+1), duration, uint32(len(payload)), flags, dts, 0)
	mv.WriteData(payload)
	return mv.Bytes()
}
