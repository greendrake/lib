package wire

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
)

// Bitmap is a picture of one bit a pixel: Height rows of Width/8 bytes each, the most
// significant bit the leftmost pixel, a set bit drawn.
type Bitmap struct {
	Width, Height int
	Rows          []byte
}

// bitmapHeaderSize is the size of what precedes a bitmap's rows in a TitleBitmapSet
// body: its width and height, each a little-endian uint16, and 12 bytes the device
// leaves unread.
const bitmapHeaderSize = 16

// EncodeBitmap is the TitleBitmapSet body of b.
func EncodeBitmap(b Bitmap) ([]byte, error) {
	if err := b.check(); err != nil {
		return nil, err
	}
	body := make([]byte, bitmapHeaderSize, bitmapHeaderSize+len(b.Rows))
	binary.LittleEndian.PutUint16(body, uint16(b.Width))
	binary.LittleEndian.PutUint16(body[2:], uint16(b.Height))
	return append(body, b.Rows...), nil
}

// DecodeBitmap is the bitmap a TitleBitmapSet body holds.
func DecodeBitmap(body []byte) (Bitmap, error) {
	if len(body) < bitmapHeaderSize {
		return Bitmap{}, fmt.Errorf("dvrip: a title bitmap body of %d bytes", len(body))
	}
	if slices.ContainsFunc(body[4:bitmapHeaderSize], func(b byte) bool { return b != 0 }) {
		return Bitmap{}, fmt.Errorf("dvrip: a title bitmap header of % x", body[:bitmapHeaderSize])
	}
	b := Bitmap{Width: int(binary.LittleEndian.Uint16(body)), Height: int(binary.LittleEndian.Uint16(body[2:])), Rows: body[bitmapHeaderSize:]}
	if err := b.check(); err != nil {
		return Bitmap{}, err
	}
	return b, nil
}

// check refuses b unless its rows are of its size, a size a TitleBitmapSet body holds.
func (b Bitmap) check() error {
	if b.Width <= 0 || b.Width%8 != 0 || b.Width > math.MaxUint16 || b.Height <= 0 || b.Height > math.MaxUint16 || len(b.Rows) != b.Width/8*b.Height {
		return fmt.Errorf("dvrip: no %d×%d bitmap is %d bytes", b.Width, b.Height, len(b.Rows))
	}
	return nil
}
