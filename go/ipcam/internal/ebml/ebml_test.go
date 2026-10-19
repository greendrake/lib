package ebml

import (
	"bytes"
	"testing"
)

// A size takes as few bytes as hold it, short of all their bits set, which is the unknown
// size.
func TestSizes(t *testing.T) {
	for _, test := range []struct {
		size uint64
		want []byte
	}{
		{0, []byte{0x80}},
		{126, []byte{0xfe}},
		{127, []byte{0x40, 0x7f}},
		{16382, []byte{0x7f, 0xfe}},
		{16383, []byte{0x20, 0x3f, 0xff}},
	} {
		b := AppendSize(nil, test.size)
		if !bytes.Equal(b, test.want) {
			t.Fatalf("size %d as % x", test.size, b)
		}
		if size, unknown, n, err := ReadSize(b); err != nil || unknown || n != len(b) || size != test.size {
			t.Fatalf("% x read as %d, unknown %v, of %d bytes: %v", b, size, unknown, n, err)
		}
	}
	if b := AppendSize8(nil, 5); !bytes.Equal(b, []byte{1, 0, 0, 0, 0, 0, 0, 5}) {
		t.Fatalf("an 8-byte size of % x", b)
	}
	if _, unknown, n, err := ReadSize(UnknownSize); err != nil || !unknown || n != 8 {
		t.Fatalf("the unknown size read as unknown %v, of %d bytes: %v", unknown, n, err)
	}
}

func TestIDs(t *testing.T) {
	for _, id := range []uint32{Void, Seek, TimestampScale, Segment} {
		b := AppendID(nil, id)
		if got, n, err := ReadID(b); err != nil || got != id || n != len(b) {
			t.Fatalf("%x written as % x, read as %x of %d bytes: %v", id, b, got, n, err)
		}
	}
}

func TestUnreadable(t *testing.T) {
	for _, b := range [][]byte{nil, {0}, {0x40}, {0x08, 0, 0, 0, 0}} {
		if _, _, err := ReadID(b); err == nil {
			t.Errorf("an ID read off % x", b)
		}
	}
	for _, b := range [][]byte{nil, {0}, {0x40}} {
		if _, _, _, err := ReadSize(b); err == nil {
			t.Errorf("a size read off % x", b)
		}
	}
}

// An unsigned integer takes as few bytes as hold it, and a Void the bytes it is of.
func TestData(t *testing.T) {
	for v, want := range map[uint64][]byte{
		0:   {0xd7, 0x81, 0},
		255: {0xd7, 0x81, 0xff},
		256: {0xd7, 0x82, 1, 0},
	} {
		if b := AppendUint(nil, TrackNumber, v); !bytes.Equal(b, want) {
			t.Errorf("%d as % x", v, b)
		}
	}
	void := AppendVoid(nil, 11)
	if size, _, n, _ := ReadSize(void[1:]); len(void) != 11 || void[0] != Void || 1+n+int(size) != 11 {
		t.Fatalf("a Void of 11 bytes as % x", void)
	}
}
