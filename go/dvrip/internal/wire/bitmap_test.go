package wire

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestBitmapRoundTrip(t *testing.T) {
	b := Bitmap{Width: 16, Height: 3, Rows: []byte{0x80, 0x01, 0xff, 0x00, 0x18, 0x81}}
	body, err := EncodeBitmap(b)
	if want := append(append([]byte{16, 0, 3, 0}, make([]byte, 12)...), b.Rows...); err != nil || !bytes.Equal(body, want) {
		t.Fatalf("encoded % x, %v", body, err)
	}
	if decoded, err := DecodeBitmap(body); err != nil || !reflect.DeepEqual(decoded, b) {
		t.Fatalf("decoded %+v, %v", decoded, err)
	}
}

func TestEncodeBitmapRefusesWhatIsNoBitmap(t *testing.T) {
	for _, b := range []Bitmap{
		{Width: 12, Height: 1, Rows: []byte{0, 0}},
		{Width: 0, Height: 1},
		{Width: 8, Height: 0},
		{Width: 8, Height: 2, Rows: []byte{0}},
		{Width: 1 << 16, Height: 1, Rows: make([]byte, 1<<13)},
		{Width: 8, Height: 1 << 16, Rows: make([]byte, 1<<16)},
	} {
		if _, err := EncodeBitmap(b); err == nil {
			t.Errorf("encoded a %d×%d bitmap of %d bytes", b.Width, b.Height, len(b.Rows))
		}
	}
}

func TestDecodeBitmapRefusesWhatIsNoBitmap(t *testing.T) {
	body, err := EncodeBitmap(Bitmap{Width: 8, Height: 1, Rows: []byte{0x81}})
	if err != nil {
		t.Fatal(err)
	}
	reserved := slices.Clone(body)
	reserved[4] = 1
	for name, test := range map[string]struct {
		body []byte
		want string
	}{
		"no header":    {body[:bitmapHeaderSize-1], "body of 15 bytes"},
		"reserved set": {reserved, "header of 08 00 01 00 01 00"},
		"rows short":   {body[:bitmapHeaderSize], "no 8×1 bitmap is 0 bytes"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeBitmap(test.body); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want an error about %q", err, test.want)
			}
		})
	}
}
