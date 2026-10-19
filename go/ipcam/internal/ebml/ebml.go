// Package ebml is the EBML (RFC 8794) of the Matroska elements a recording has (RFC 9559),
// for both its writer and the reader of its tests: their IDs, and the encoding of their
// IDs, sizes and data.
package ebml

import (
	"errors"
	"math"
	"math/bits"
	"time"
)

// The elements' IDs, their length markers included.
const (
	EBML               = 0x1a45dfa3
	EBMLVersion        = 0x4286
	EBMLReadVersion    = 0x42f7
	EBMLMaxIDLength    = 0x42f2
	EBMLMaxSizeLength  = 0x42f3
	DocType            = 0x4282
	DocTypeVersion     = 0x4287
	DocTypeReadVersion = 0x4285
	Void               = 0xec

	Segment            = 0x18538067
	SeekHead           = 0x114d9b74
	Seek               = 0x4dbb
	SeekID             = 0x53ab
	SeekPosition       = 0x53ac
	Info               = 0x1549a966
	TimestampScale     = 0x2ad7b1
	Duration           = 0x4489
	DateUTC            = 0x4461
	MuxingApp          = 0x4d80
	WritingApp         = 0x5741
	Tracks             = 0x1654ae6b
	TrackEntry         = 0xae
	TrackNumber        = 0xd7
	TrackUID           = 0x73c5
	TrackType          = 0x83
	CodecID            = 0x86
	CodecPrivate       = 0x63a2
	Video              = 0xe0
	PixelWidth         = 0xb0
	PixelHeight        = 0xba
	Audio              = 0xe1
	SamplingFrequency  = 0xb5
	Channels           = 0x9f
	Cluster            = 0x1f43b675
	Timestamp          = 0xe7
	SimpleBlock        = 0xa3
	Cues               = 0x1c53bb6b
	CuePoint           = 0xbb
	CueTime            = 0xb3
	CueTrackPositions  = 0xb7
	CueTrack           = 0xf7
	CueClusterPosition = 0xf1
)

// UnknownSize is the 8-byte size of an element whose size is not known: all its bits set.
var UnknownSize = []byte{0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

// Epoch is when a date counts from.
var Epoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// appendBigEndian appends v's low n bytes, big-endian.
func appendBigEndian(b []byte, v uint64, n int) []byte {
	for i := n - 1; i >= 0; i-- {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

// bytesOf is how many bytes hold v: one at least.
func bytesOf(v uint64) int {
	return max(1, (bits.Len64(v)+7)/8)
}

// AppendID appends id as it is written: its bytes, past any leading zero ones.
func AppendID(b []byte, id uint32) []byte {
	return appendBigEndian(b, uint64(id), bytesOf(uint64(id)))
}

// AppendSize appends size as a data size of as few bytes as hold it.
func AppendSize(b []byte, size uint64) []byte {
	n := 1
	// A size of all its bits set is the unknown one.
	for n < 8 && size >= 1<<(7*n)-1 {
		n++
	}
	return appendSizeOf(b, size, n)
}

// AppendSize8 appends size as a data size of 8 bytes: one that can be written over by
// another.
func AppendSize8(b []byte, size uint64) []byte {
	return appendSizeOf(b, size, 8)
}

// appendSizeOf appends size as a data size of n bytes, behind its length marker.
func appendSizeOf(b []byte, size uint64, n int) []byte {
	return appendBigEndian(b, size|1<<(7*n), n)
}

// AppendElement appends the element of id holding data.
func AppendElement(b []byte, id uint32, data []byte) []byte {
	b = AppendID(b, id)
	b = AppendSize(b, uint64(len(data)))
	return append(b, data...)
}

// AppendUint appends the element of id holding v, in as few bytes as hold it.
func AppendUint(b []byte, id uint32, v uint64) []byte {
	return AppendElement(b, id, appendBigEndian(nil, v, bytesOf(v)))
}

// AppendUint8 appends the element of id holding v in 8 bytes: one that can be written over
// by another.
func AppendUint8(b []byte, id uint32, v uint64) []byte {
	return AppendElement(b, id, appendBigEndian(nil, v, 8))
}

// AppendFloat appends the element of id holding f, in 8 bytes.
func AppendFloat(b []byte, id uint32, f float64) []byte {
	return AppendUint8(b, id, math.Float64bits(f))
}

// AppendString appends the element of id holding s.
func AppendString(b []byte, id uint32, s string) []byte {
	return AppendElement(b, id, []byte(s))
}

// AppendDate appends the element of id holding t, as nanoseconds since 2001.
func AppendDate(b []byte, id uint32, t time.Time) []byte {
	return AppendUint8(b, id, uint64(t.Sub(Epoch).Nanoseconds()))
}

// AppendVoid appends a Void of size bytes in all, its ID and 8-byte data size included: what
// holds the place of an element of that size until it is written. No Void takes fewer
// than 9.
func AppendVoid(b []byte, size int) []byte {
	b = AppendID(b, Void)
	b = AppendSize8(b, uint64(size-9))
	return append(b, make([]byte, size-9)...)
}

// Uint is the unsigned integer b holds, big-endian: 0 for none.
func Uint(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

// vintLength is how many bytes the variable-length integer b starts with takes, as the
// leading zero bits of its first byte say, and whether b holds them.
func vintLength(b []byte) (int, bool) {
	if len(b) == 0 {
		return 0, false
	}
	n := bits.LeadingZeros8(b[0]) + 1
	return n, n <= 8 && len(b) >= n
}

// ReadID reads an element's ID off b: the ID, and how many bytes it takes.
func ReadID(b []byte) (uint32, int, error) {
	n, held := vintLength(b)
	if !held || n > 4 {
		return 0, 0, errors.New("ebml: no ID")
	}
	return uint32(Uint(b[:n])), n, nil
}

// ReadSize reads an element's data size off b: the size, whether it is unknown, and how
// many bytes it takes.
func ReadSize(b []byte) (uint64, bool, int, error) {
	n, held := vintLength(b)
	if !held {
		return 0, false, 0, errors.New("ebml: no size")
	}
	marker := uint64(1) << (7 * n)
	size := Uint(b[:n]) &^ marker
	return size, size == marker-1, n, nil
}
