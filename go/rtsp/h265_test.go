package rtsp

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"

	"greendrake.nz/go/rtsp/rtsptest"
)

// NAL units of the types the tests send, each a header and a byte of payload.
var (
	vps   = []byte{32 << 1, 1, 0x0c}
	sps   = []byte{33 << 1, 1, 0x01}
	idr   = []byte{19 << 1, 1, 0xaf}
	trail = []byte{1 << 1, 1, 0xbe}
)

// pusher sends packets in sequence.
type pusher struct {
	h        H265
	sequence uint16
}

func (p *pusher) push(timestamp uint32, marker bool, payload []byte) [][]byte {
	nalus, at := p.h.Push(Packet{Sequence: p.sequence, Timestamp: timestamp, Marker: marker, Payload: payload})
	p.sequence++
	if nalus != nil && at != timestamp {
		panic("a unit of another timestamp")
	}
	return nalus
}

// aggregation is an aggregation packet of nalus.
func aggregation(nalus ...[]byte) []byte {
	b := []byte{48 << 1, 1}
	for _, nalu := range nalus {
		b = append(b, byte(len(nalu)>>8), byte(len(nalu)))
		b = append(b, nalu...)
	}
	return b
}

func same(t *testing.T, got [][]byte, want ...[]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("units %x, want %x", got, want)
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("units %x, want %x", got, want)
		}
	}
}

func TestSingleNALUnits(t *testing.T) {
	var p pusher
	if p.push(10, false, vps) != nil {
		t.Fatal("a unit before its marker")
	}
	same(t, p.push(10, true, idr), vps, idr)
	same(t, p.push(20, true, trail), trail)
}

func TestAggregationPackets(t *testing.T) {
	var p pusher
	same(t, p.push(10, true, aggregation(vps, sps, idr)), vps, sps, idr)
	// One that says its unit runs past its end is broken, as is one of a unit too short
	// for its header.
	past := aggregation(vps)
	past[3] = 9
	for i, broken := range [][]byte{past, aggregation(vps, []byte{}), aggregation(vps, []byte{1})} {
		if p.push(uint32(20+i), true, broken) != nil {
			t.Fatalf("broken aggregation % x went through", broken)
		}
	}
	same(t, p.push(30, true, trail), trail)
}

func TestParameterSets(t *testing.T) {
	sets, err := ParameterSets(Media{Params: map[string]string{
		"sprop-vps": base64.StdEncoding.EncodeToString(vps),
		// Some cameras put a start code ahead of a set; a parameter can be a list.
		"sprop-sps": base64.StdEncoding.EncodeToString(append([]byte{0, 0, 0, 1}, sps...)) + "," + base64.StdEncoding.EncodeToString(append([]byte{0, 0, 1}, sps...)),
		"sprop-pps": "",
	}})
	if err != nil {
		t.Fatal(err)
	}
	same(t, sets, vps, sps, sps)
	if _, err := ParameterSets(Media{Params: map[string]string{"sprop-pps": "not base64!"}}); err == nil {
		t.Fatal("read a set of no encoding")
	}
	if NALType(sps) != 33 {
		t.Fatalf("type %d", NALType(sps))
	}
}

func TestFragmentationUnits(t *testing.T) {
	long := []byte{19 << 1, 1, 1, 2, 3, 4}
	var p pusher
	// A byte of the unit each.
	fus := rtsptest.Payloads(long, 4)
	for _, fu := range fus[:len(fus)-1] {
		if p.push(10, false, fu) != nil {
			t.Fatal("a unit before its end")
		}
	}
	same(t, p.push(10, true, fus[len(fus)-1]), long)
	// One that starts part way through is broken.
	if p.push(20, true, fus[1]) != nil {
		t.Fatal("a fragment with no start went through")
	}
	// As is one whose unit is marked done before its end.
	p.push(30, false, fus[0])
	if p.push(30, true, fus[1]) != nil {
		t.Fatal("an unfinished fragment went through")
	}
}

// A unit a packet of which went missing is dropped, and the next one taken.
func TestLostPackets(t *testing.T) {
	var p pusher
	p.push(10, false, vps)
	p.sequence++
	if p.push(10, true, idr) != nil {
		t.Fatal("a unit that lost a packet went through")
	}
	same(t, p.push(20, true, trail), trail)
	// Packets missing between two units take the next unit with them: they can have been
	// its.
	p.push(30, false, vps)
	p.sequence++
	if p.push(50, true, trail) != nil {
		t.Fatal("a unit after lost packets went through")
	}
	// A unit left unmarked gives way to the next.
	p.push(60, false, vps)
	same(t, p.push(70, true, trail), trail)
}

func TestMalformedPayloads(t *testing.T) {
	var p pusher
	for _, payload := range [][]byte{{1}, {49 << 1, 1}, {50 << 1, 1, 0}} {
		if p.push(uint32(p.sequence), true, payload) != nil {
			t.Fatalf("% x went through", payload)
		}
	}
}

// clockStep is a timestamp, and the time it is.
type clockStep struct {
	timestamp uint32
	want      time.Duration
}

// timed fails t unless c times each step as it wants.
func timed(t *testing.T, c Clock, steps []clockStep) {
	t.Helper()
	for _, step := range steps {
		got, err := c.Time(step.timestamp)
		// A frame rate's times are a nanosecond's rounding apart from the wanted.
		if err != nil || (got-step.want).Abs() > time.Nanosecond {
			t.Fatalf("time of %d is %v, %v, want %v", step.timestamp, got, err, step.want)
		}
	}
}

func TestClock(t *testing.T) {
	timed(t, Media{ClockRate: 90000}.Clock(), []clockStep{
		{4294960000, 0},
		// Wrapping round.
		{2000, (2000 + 7296) * time.Second / 90000},
		// A step back.
		{1100, 8396 * time.Second / 90000},
	})
	// Days of a stream, past where ticks times a second would overflow.
	timed(t, Clock{rate: 90000, started: true, last: 4294967295, ticks: 90000*86400*4 - 1}, []clockStep{{0, 4 * 24 * time.Hour}})
}

// A stream whose timestamps stand still is timed by its frame rate from then on, whatever
// its timestamps do after.
func TestStillClock(t *testing.T) {
	timed(t, Media{ClockRate: 90000, FrameRate: 15}.Clock(), []clockStep{
		{0, 0},
		{4500, 50 * time.Millisecond},
		{4500, 50*time.Millisecond + time.Second/15},
		{4500, 50*time.Millisecond + 2*time.Second/15},
		{9000, 50*time.Millisecond + 3*time.Second/15},
	})
	c := Media{ClockRate: 90000}.Clock()
	if _, err := c.Time(0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Time(0); err == nil {
		t.Fatal("timestamps that stand still timed with no frame rate")
	}
}
