package rtspcam

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/bluenviron/mediacommon/pkg/codecs/h264"
	mediah265 "github.com/bluenviron/mediacommon/pkg/codecs/h265"
	"greendrake.nz/go/camstream"
	"greendrake.nz/go/camstream/h265"
	"greendrake.nz/go/rtsp"
)

// frameTimeout is the most silence a playing stream may keep before it is taken for
// dead.
const frameTimeout = 5 * time.Second

// monitor is a stream played on a connection of its own: its video alone.
type monitor struct {
	conn  *rtsp.Conn
	units rtsp.H265
	clock rtsp.Clock
	// The parameter sets by type, which every random access unit goes out with: the
	// description's, and then the stream's own.
	sets map[mediah265.NALUType][][]byte
	// A frame lasts until the next one is due, so each is held back until the next
	// arrives to say when that is.
	held *camstream.Frame
}

// play plays the H.265 stream at path, on the camera at address, its packets coming over
// transport. ctx bounds the opening alone.
func play(ctx context.Context, address, path, user, password string, transport rtsp.Transport) (*monitor, error) {
	conn, err := rtsp.Dial(ctx, address, user, password)
	if err != nil {
		return nil, err
	}
	m, err := func() (*monitor, error) {
		description, err := conn.Describe(ctx, path)
		if err != nil {
			return nil, err
		}
		media, has := description.Video()
		if !has || media.Encoding != "H265" {
			return nil, errors.New("rtspcam: the stream has no H.265 video")
		}
		sets, err := rtsp.ParameterSets(media)
		if err != nil {
			return nil, err
		}
		m := &monitor{conn: conn, clock: media.Clock(), sets: map[mediah265.NALUType][][]byte{}}
		m.keep(sets)
		return m, conn.Play(ctx, description, media, transport)
	}()
	if err != nil {
		// The connection is the camera's to hand back, whatever failed on it.
		conn.Close()
		return nil, err
	}
	return m, nil
}

// keep takes parameter sets, those of each type in place of the type's kept.
func (m *monitor) keep(sets [][]byte) {
	fresh := map[mediah265.NALUType][][]byte{}
	for _, set := range sets {
		fresh[h265.TypeOf(set)] = append(fresh[h265.TypeOf(set)], set)
	}
	maps.Copy(m.sets, fresh)
}

// ShutDown closes the connection, which is also what unblocks a Next waiting on it.
func (m *monitor) ShutDown() {
	m.conn.Close()
}

// HasAudio is false: the video alone is set up.
func (m *monitor) HasAudio() bool {
	return false
}

func (m *monitor) Next() (*camstream.Frame, error) {
	quiet := time.NewTimer(frameTimeout)
	defer quiet.Stop()
	for {
		select {
		case p, live := <-m.conn.Packets():
			if !live {
				return nil, m.conn.Err()
			}
			nalus, timestamp := m.units.Push(p)
			if nalus == nil {
				continue
			}
			f, err := m.frameOf(nalus)
			if err != nil {
				return nil, err
			}
			if f == nil {
				continue
			}
			// Frames alone are timed: a clock that has stood still counts them.
			if f.PTS, err = m.clock.Time(timestamp); err != nil {
				return nil, err
			}
			previous := m.held
			m.held = f
			if previous == nil {
				continue
			}
			previous.Duration = f.PTS - previous.PTS
			return previous, nil
		case <-quiet.C:
			return nil, errors.New("rtspcam: the stream went quiet")
		}
	}
}

// frameOf is the frame of an access unit, its time aside: nil for one of parameter sets
// alone, which are kept to go out ahead of the next random access unit.
func (m *monitor) frameOf(au [][]byte) (*camstream.Frame, error) {
	var units, sets [][]byte
	for _, nalu := range au {
		switch kind := h265.TypeOf(nalu); {
		case slices.Contains(h265.ParameterSets, kind):
			sets = append(sets, nalu)
		case kind != mediah265.NALUType_AUD_NUT:
			units = append(units, nalu)
		}
	}
	m.keep(sets)
	if units == nil {
		return nil, nil
	}
	random := mediah265.IsRandomAccess(units)
	if random {
		var ahead [][]byte
		for _, kind := range h265.ParameterSets {
			ahead = append(ahead, m.sets[kind]...)
		}
		units = append(ahead, units...)
	}
	data, err := h264.AnnexB(units).Marshal()
	if err != nil {
		return nil, err
	}
	return &camstream.Frame{Kind: camstream.Video, Codec: camstream.H265, Keyframe: random, Data: data}, nil
}
