package rtsp

import (
	"reflect"
	"strings"
	"testing"
)

// A camera's description, as HIK Media Server-like ones come: an aggregate control of *,
// each media's relative to the base.
const cameraSDP = `v=0
o=- 1109162014219182 0 IN IP4 0.0.0.0
s=Media Server
c=IN IP4 0.0.0.0
t=0 0
a=control:*
a=range:npt=now-
m=video 0 RTP/AVP 96
a=rtpmap:96 H265/90000
a=fmtp:96 profile-id=1;sprop-vps=QAEMAf//AWAAAAMAsAAAAwAAAwBdqwJA;sprop-sps=QgEBAWAAAAMAsAAAAwAAAwBdoAKAgC0WNrkky/AIAAADAAgAAAMBlCA=;sprop-pps=RAHA8vA8kAA=
a=rtpmap:97 H264/90000
a=control:trackID=video
m=audio 0 RTP/AVP 8
a=control:trackID=audio
`

func TestDescription(t *testing.T) {
	d, err := parseDescription([]byte(strings.ReplaceAll(cameraSDP, "\n", "\r\n")), "rtsp://cam/0/")
	if err != nil {
		t.Fatal(err)
	}
	want := &Description{Control: "rtsp://cam/0/", Medias: []Media{
		{Type: "video", Control: "rtsp://cam/0/trackID=video", PayloadType: 96, Encoding: "H265", ClockRate: 90000, Params: map[string]string{
			"profile-id": "1",
			"sprop-vps":  "QAEMAf//AWAAAAMAsAAAAwAAAwBdqwJA",
			"sprop-sps":  "QgEBAWAAAAMAsAAAAwAAAwBdoAKAgC0WNrkky/AIAAADAAgAAAMBlCA=",
			"sprop-pps":  "RAHA8vA8kAA=",
		}},
		// A payload type of RFC 3551's own, which needs no rtpmap.
		{Type: "audio", Control: "rtsp://cam/0/trackID=audio", PayloadType: 8, Encoding: "PCMA", ClockRate: 8000, Params: map[string]string{}},
	}}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("%+v", d)
	}
}

// A BITVISION camera's description of its extra stream, as it gave it, under a
// Content-Base of rtsp://192.0.2.132/1: the base names no trailing slash.
const bitvisionSDP = `v=0
o=StreamingServer 3331435948 1116907222000 IN IP4 192.0.2.33
s=h264.mp4
c=IN IP4 0.0.0.0
t=0 0
a=control:*
m=video 0 RTP/AVP 96
a=control:trackID=0
a=rtpmap:96 H265/90000
a=videoinfo:720*480*15*4096
m=audio 0 RTP/AVP 0
a=control:trackID=1
a=rtpmap:0 PCMU/8000
a=ptime:20
`

func TestBITVISIONDescription(t *testing.T) {
	d, err := parseDescription([]byte(strings.ReplaceAll(bitvisionSDP, "\n", "\r\n")), "rtsp://192.0.2.132/1")
	if err != nil {
		t.Fatal(err)
	}
	want := &Description{Control: "rtsp://192.0.2.132/1", Medias: []Media{
		// Its frame rate in BITVISION's own attribute.
		{Type: "video", Control: "rtsp://192.0.2.132/1/trackID=0", PayloadType: 96, Encoding: "H265", ClockRate: 90000, Params: map[string]string{}, FrameRate: 15},
		{Type: "audio", Control: "rtsp://192.0.2.132/1/trackID=1", PayloadType: 0, Encoding: "PCMU", ClockRate: 8000, Params: map[string]string{}},
	}}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("%+v", d)
	}
}

func TestVideo(t *testing.T) {
	d := &Description{Medias: []Media{{Type: "audio", Encoding: "PCMU"}, {Type: "video", Encoding: "H265"}, {Type: "video", Encoding: "H264"}}}
	if video, has := d.Video(); !has || video.Encoding != "H265" {
		t.Fatalf("%+v, %v", video, has)
	}
	d.Medias = d.Medias[:1]
	if video, has := d.Video(); has {
		t.Fatalf("%+v of audio alone", video)
	}
}

func TestControls(t *testing.T) {
	d, err := parseDescription([]byte("a=control:rtsp://cam/live\nm=video 0 RTP/AVP 26\na=control:rtsp://cam/live/v\nm=video 0 RTP/AVP 96\n"), "rtsp://cam/0")
	if err != nil {
		t.Fatal(err)
	}
	// A media that names no control is set up where the stream is described.
	if d.Control != "rtsp://cam/live" || d.Medias[0].Control != "rtsp://cam/live/v" || d.Medias[0].Encoding != "JPEG" || d.Medias[1].Control != "rtsp://cam/0" || d.Medias[1].Encoding != "" {
		t.Fatalf("%+v", d)
	}
	if got := resolve("rtsp://cam/0", "track1"); got != "rtsp://cam/0/track1" {
		t.Fatalf("resolved %s", got)
	}
}

// RFC 4566's frame rate, which may be a fraction. What is no SDP line, and another
// format's parameters, go unread.
func TestFrameRate(t *testing.T) {
	d, err := parseDescription([]byte("junk\nm=video 0 RTP/AVP 96\na=fmtp:97 x=1\na=framerate:29.97\n"), "rtsp://cam/0")
	if err != nil || d.Medias[0].FrameRate != 29.97 || len(d.Medias[0].Params) != 0 {
		t.Fatalf("%+v, %v", d, err)
	}
}

func TestDescriptionErrors(t *testing.T) {
	for _, sdp := range []string{
		"m=video 0 RTP/AVP\n",
		"m=video 0 RTP/AVP x\n",
		"m=video 0 RTP/AVP 96\na=rtpmap:96 H265/fast\n",
		"m=video 0 RTP/AVP 96\na=framerate:0\n",
		"m=video 0 RTP/AVP 96\na=videoinfo:720*480\n",
	} {
		if _, err := parseDescription([]byte(sdp), "rtsp://cam/0"); err == nil {
			t.Errorf("%q read", sdp)
		}
	}
}
