package dvrip

import (
	"context"
	"time"

	"greendrake.nz/go/dvrip/internal/wire"
)

// SystemInfo reads what the device reports about itself.
func (s *Session) SystemInfo(ctx context.Context) (SystemInfo, error) {
	return decodeAs[SystemInfo](s.Info(ctx, "SystemInfo"))
}

// Time reads the device's clock. The device keeps wall time in no zone, written as
// time.DateTime, so it comes back as that wall time in UTC.
func (s *Session) Time(ctx context.Context) (time.Time, error) {
	text, err := decodeAs[string](s.query(ctx, wire.TimeQuery, "OPTimeQuery"))
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.DateTime, text)
}

// SetTime sets the device's clock to t's wall time.
func (s *Session) SetTime(ctx context.Context, t time.Time) error {
	_, err := s.command(ctx, wire.SystemManager, "OPTimeSetting", t.Format(time.DateTime))
	return err
}

// Reboot restarts the device, which ends the session.
func (s *Session) Reboot(ctx context.Context) error {
	_, err := s.command(ctx, wire.SystemManager, "OPMachine", map[string]string{"Action": "Reboot"})
	return err
}

// Bitmap is a picture of one bit a pixel: Height rows of Width/8 bytes each, the most
// significant bit the leftmost pixel, a set bit drawn.
type Bitmap = wire.Bitmap

// DrawTitle has the device draw b as the channel title over its picture, where it
// keeps it. The device draws no title text of its own: the title's text is kept as
// VideoWidget's, and what shows is the bitmap sent last, scaled with each stream.
func (s *Session) DrawTitle(ctx context.Context, b Bitmap) error {
	body, err := wire.EncodeBitmap(b)
	if err != nil {
		return err
	}
	data, err := s.request(ctx, wire.TitleBitmapSet, body)
	if err != nil {
		return err
	}
	_, err = decodeReply(data, "title bitmap")
	return err
}

// ExportConfig fetches the device's whole configuration as the zip archive it exports.
func (s *Session) ExportConfig(ctx context.Context) ([]byte, error) {
	body, err := s.commandBody("", nil)
	if err != nil {
		return nil, err
	}
	return s.request(ctx, wire.ConfigExport, body)
}
