package dvripcam

import (
	"context"
	"fmt"
	"image"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"greendrake.nz/go/dvrip"
	"greendrake.nz/go/ipcam"
)

// The cameras draw no title text of their own: the client renders it. titleSize is the
// size it is rendered at, in pixels: the cameras' own date and time on their smallest
// stream, which they scale, title and time alike, for the larger ones.
const titleSize = 16

// titleFont is Go Regular, which the title is rendered in.
var titleFont = func() *opentype.Font {
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		panic(err)
	}
	return f
}()

// drawing is what a write of the camera's name also does: draw it as the title.
func drawing(value any) (func(ctx context.Context, s *dvrip.Session) error, error) {
	name, err := as[string](value)
	if err != nil {
		return nil, err
	}
	title, err := titleOf(name)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, s *dvrip.Session) error { return s.DrawTitle(ctx, title) }, nil
}

// titleOf renders name as the bitmap a camera draws for its title: white on nothing, a
// pixel a bit, as wide as the name in whole bytes. A name with a character the font has
// no glyph for is InvalidValue.
func titleOf(name string) (dvrip.Bitmap, error) {
	var buffer sfnt.Buffer
	for _, r := range name {
		if glyph, err := titleFont.GlyphIndex(&buffer, r); err != nil || glyph == 0 {
			return dvrip.Bitmap{}, &ipcam.InvalidValue{Problem: fmt.Sprintf("%q is a character the camera's title cannot be drawn in", r)}
		}
	}
	// A face is for one goroutine at a time.
	face, err := opentype.NewFace(titleFont, &opentype.FaceOptions{Size: titleSize, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return dvrip.Bitmap{}, err
	}
	defer face.Close()
	metrics := face.Metrics()
	width := (font.MeasureString(face, name).Ceil() + 7) / 8 * 8
	height := (metrics.Ascent + metrics.Descent).Ceil()
	picture := image.NewGray(image.Rect(0, 0, width, height))
	draw.Draw(picture, picture.Bounds(), image.Black, image.Point{}, draw.Src)
	drawer := font.Drawer{Dst: picture, Src: image.White, Face: face, Dot: fixed.P(0, metrics.Ascent.Ceil())}
	drawer.DrawString(name)
	rows := make([]byte, 0, width/8*height)
	for y := range height {
		for x := 0; x < width; x += 8 {
			var b byte
			for i := range 8 {
				b <<= 1
				if picture.GrayAt(x+i, y).Y > 128 {
					b |= 1
				}
			}
			rows = append(rows, b)
		}
	}
	return dvrip.Bitmap{Width: width, Height: height, Rows: rows}, nil
}
