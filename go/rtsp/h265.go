package rtsp

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"time"

	"greendrake.nz/go/rtsp/internal/wire"
)

// The RTP payload's own NAL unit types (RFC 7798 4.4).
const (
	aggregationPacket   = 48
	fragmentationUnit   = 49
	contentInfoAndPACIs = 50
)

// NALType is the type of an H.265 NAL unit, by its header.
func NALType(nalu []byte) byte {
	return wire.NALType(nalu)
}

// ParameterSets are the H.265 parameter sets media's description carries (RFC 7798 7.1),
// VPS, SPS and PPS in that order, each of a list. An Annex-B start code some cameras put
// ahead of a set is left off.
func ParameterSets(media Media) ([][]byte, error) {
	var sets [][]byte
	for _, key := range []string{"sprop-vps", "sprop-sps", "sprop-pps"} {
		for _, encoded := range strings.Split(media.Params[key], ",") {
			if encoded == "" {
				continue
			}
			set, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return nil, err
			}
			set = bytes.TrimPrefix(set, []byte{0, 0, 0, 1})
			sets = append(sets, bytes.TrimPrefix(set, []byte{0, 0, 1}))
		}
	}
	return sets, nil
}

// H265 puts an H.265 stream's access units back together from its RTP packets (RFC
// 7798): single NAL units, aggregation packets and fragmentation units, a unit ending at
// its packet with the marker set. A unit a packet of which went missing is dropped.
type H265 struct {
	nalus [][]byte
	// The timestamp of the unit so far.
	timestamp uint32
	// A fragmented NAL unit so far.
	fragment []byte
	// The sequence number the next packet carries, once a packet has come.
	next    uint16
	started bool
	// Set once the unit so far has lost a packet.
	broken bool
}

// Push takes the stream's next packet, and answers the access unit it completes, as its
// NAL units, with its timestamp; nil while it completes none.
func (h *H265) Push(p Packet) ([][]byte, uint32) {
	// A timestamp of its own starts a unit, whatever is left of the last, which lost its
	// marked packet.
	if p.Timestamp != h.timestamp {
		h.nalus, h.fragment, h.broken = nil, nil, false
	}
	if h.started && p.Sequence != h.next {
		h.broken, h.fragment = true, nil
	}
	h.started, h.next, h.timestamp = true, p.Sequence+1, p.Timestamp
	h.take(p.Payload)
	if !p.Marker {
		return nil, 0
	}
	nalus, broken := h.nalus, h.broken || h.fragment != nil
	h.nalus, h.fragment, h.broken = nil, nil, false
	if broken || len(nalus) == 0 {
		return nil, 0
	}
	return nalus, p.Timestamp
}

// take takes the NAL units of a packet's payload into the unit so far.
func (h *H265) take(payload []byte) {
	if len(payload) < 2 {
		h.broken = true
		return
	}
	switch wire.NALType(payload) {
	case aggregationPacket:
		for rest := payload[2:]; len(rest) > 0; {
			if len(rest) < 2 {
				h.broken = true
				return
			}
			// A NAL unit is its two-byte header at least.
			size := int(binary.BigEndian.Uint16(rest))
			if size < 2 || size > len(rest)-2 {
				h.broken = true
				return
			}
			h.nalus = append(h.nalus, rest[2:2+size])
			rest = rest[2+size:]
		}
	case fragmentationUnit:
		if len(payload) < 3 {
			h.broken = true
			return
		}
		start, end, kind := payload[2]&0x80 != 0, payload[2]&0x40 != 0, payload[2]&0x3f
		switch {
		case start:
			// The NAL unit's own header: the payload header's, with the unit's type.
			h.fragment = append([]byte{payload[0]&0x81 | kind<<1, payload[1]}, payload[3:]...)
		case h.fragment == nil:
			h.broken = true
			return
		default:
			h.fragment = append(h.fragment, payload[3:]...)
		}
		if end {
			h.nalus = append(h.nalus, h.fragment)
			h.fragment = nil
		}
	case contentInfoAndPACIs:
		h.broken = true
	default:
		h.nalus = append(h.nalus, payload)
	}
}

// Clock is what media's RTP timestamps are timed by.
func (m Media) Clock() Clock {
	return Clock{rate: m.ClockRate, frameRate: m.FrameRate}
}

// Clock turns a media's RTP timestamps into time since its first frame's, by its clock
// rate. A media whose timestamps stand still, a frame's the same as the one before's (a
// BITVISION camera's extra stream's are all 0), is timed by its frame rate from then on:
// each frame one after the one before.
type Clock struct {
	rate int
	// 0 where the media's description gives no frame rate, which leaves timestamps that
	// stand still with no time.
	frameRate float64
	started   bool
	last      uint32
	// Ticks since the first timestamp: wider than a timestamp, which wraps in hours.
	ticks int64
	// Once the timestamps have stood still: the time they stood at, and the frames since.
	still  bool
	stood  time.Duration
	frames int
}

// Time is the time of timestamp, the next frame's: a unit of parameter sets alone is no
// frame, and timing one would count it as one where the timestamps stand still, and
// take a camera that sends one at its frame's timestamp for one whose timestamps do.
func (c *Clock) Time(timestamp uint32) (time.Duration, error) {
	if c.started && timestamp == c.last && !c.still {
		if c.frameRate == 0 {
			return 0, errors.New("rtsp: the stream's timestamps stand still, and its description gives no frame rate")
		}
		c.still, c.stood = true, c.time()
	}
	if c.still {
		c.frames++
		return c.stood + time.Duration(float64(c.frames)*float64(time.Second)/c.frameRate), nil
	}
	if c.started {
		c.ticks += int64(int32(timestamp - c.last))
	}
	c.started, c.last = true, timestamp
	return c.time(), nil
}

// time is the time of the ticks so far.
func (c *Clock) time() time.Duration {
	// Seconds and the rest apart, which keeps a stream of days within a Duration's range.
	rate := int64(c.rate)
	return time.Duration(c.ticks/rate)*time.Second + time.Duration(c.ticks%rate)*time.Second/time.Duration(rate)
}
