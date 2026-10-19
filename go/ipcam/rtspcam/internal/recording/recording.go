// Package recording is greendrake.nz/go/camstream/camstreamtest's recording as an RTSP
// camera plays it.
package recording

import (
	"github.com/bluenviron/mediacommon/pkg/codecs/h264"
	"greendrake.nz/go/camstream"
	"greendrake.nz/go/camstream/camstreamtest"
	"greendrake.nz/go/rtsp/rtsptest"
)

// Units are the recording's video, an XM530 camera's extra stream, 800×448 H.265 at 15
// frames a second, as its access units.
func Units() ([]rtsptest.Unit, error) {
	frames, err := camstreamtest.Recording()
	if err != nil {
		return nil, err
	}
	var units []rtsptest.Unit
	for _, f := range frames {
		if f.Kind != camstream.Video {
			continue
		}
		var nalus h264.AnnexB
		if err := nalus.Unmarshal(f.Data); err != nil {
			return nil, err
		}
		units = append(units, rtsptest.Unit{NALUs: nalus, PTS: f.PTS})
	}
	return units, nil
}
