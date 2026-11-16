package transcode

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	video := func(v Video) Output { return Output{Format: "mp4", Video: &v} }
	for _, o := range []Output{
		{Format: "mp4", Video: &Video{Codec: "h264", MaxShortSide: 720, Bitrate: 2_000_000, PixelFormat: "yuv420p"}, Audio: &Audio{Codec: "aac", Bitrate: 128_000}},
		{Format: "ipod", Audio: &Audio{Codec: "aac"}},
		video(Video{Codec: "mpeg4", MaxShortSide: 2}),
		video(Video{Codec: "prores"}),
	} {
		if err := o.Validate(); err != nil {
			t.Errorf("%+v: %v", o, err)
		}
	}
	for name, o := range map[string]Output{
		"neither video nor audio": {Format: "mp4"},
		"no format":               {Video: &Video{Codec: "h264"}},
		"an option as a format":   {Format: "mp4 -y", Audio: &Audio{Codec: "aac"}},
		"a path as a format":      {Format: "../mp4", Audio: &Audio{Codec: "aac"}},
		"an uppercase codec":      video(Video{Codec: "H264"}),
		"no video codec":          video(Video{}),
		"an option as a codec":    video(Video{Codec: "-c:v"}),
		"an option as a pixel":    video(Video{Codec: "h264", PixelFormat: "yuv420p,format=gray"}),
		"an odd short side":       video(Video{Codec: "h264", MaxShortSide: 719}),
		"a negative short side":   video(Video{Codec: "h264", MaxShortSide: -2}),
		"a negative video rate":   video(Video{Codec: "h264", Bitrate: -1}),
		"no audio codec":          {Format: "ipod", Audio: &Audio{}},
		"a negative audio rate":   {Format: "ipod", Audio: &Audio{Codec: "aac", Bitrate: -1}},
		"negative seconds":        {Format: "ipod", Audio: &Audio{Codec: "aac"}, MaxSeconds: -1},
	} {
		var failure *Failure
		if err := o.Validate(); !errors.As(err, &failure) {
			t.Errorf("%s: %v, no Failure", name, err)
		}
	}
}

func TestFailureError(t *testing.T) {
	for f, want := range map[Failure]string{
		{Message: "ffmpeg failed"}:                    "ffmpeg failed",
		{Message: "ffmpeg failed", Detail: "no moov"}: "ffmpeg failed: no moov",
	} {
		if got := f.Error(); got != want {
			t.Errorf("%+v: %q, want %q", f, got, want)
		}
	}
}

func TestJobReport(t *testing.T) {
	Job{}.Report(Running{}) // no Progress: nothing to tell
	var got []Progress
	job := Job{Progress: func(p Progress) { got = append(got, p) }}
	for _, s := range []Status{Queued{Ahead: 2}, Running{}, Advanced{Seconds: 1.5}} {
		job.Report(s)
	}
	want := []Progress{{Queued: 2}, {Running: true}, {Running: true, Seconds: 1.5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reported %+v, want %+v", got, want)
	}
}

func TestMessages(t *testing.T) {
	for m, want := range map[Message]string{
		Start{Label: "a.mov", Size: 10, Output: Output{Format: "ipod", Audio: &Audio{Codec: "aac", Bitrate: 96000}, MaxSeconds: 60}}: `{"type":"start","label":"a.mov","size":10,"output":{"format":"ipod","video":null,"audio":{"codec":"aac","bitrate":96000},"max_seconds":60}}`,
		Abort{Error: "gone"}: `{"type":"abort","error":"gone"}`,
		Queued{Ahead: 1}:     `{"type":"queued","ahead":1}`,
		Running{}:            `{"type":"running"}`,
		Read{ID: 3, Offset: 1 << 40, Length: 4096}:     `{"type":"read","id":3,"offset":1099511627776,"length":4096}`,
		Advanced{Seconds: 2.5}:                         `{"type":"progress","seconds":2.5}`,
		Result{Size: 7}:                                `{"type":"result","size":7}`,
		Failure{Message: "no encoder", Detail: "tail"}: `{"type":"failed","error":"no encoder","detail":"tail"}`,
	} {
		b, err := Encode(m)
		if err != nil || string(b) != want {
			t.Errorf("%+v encoded as %s, %v; want %s", m, b, err, want)
			continue
		}
		back, err := Decode(b)
		if err != nil || !reflect.DeepEqual(reflect.ValueOf(back).Elem().Interface(), m) {
			t.Errorf("%s decoded as %+v, %v", b, back, err)
		}
	}
	for _, b := range []string{
		`{"type":"seek","offset":1}`, `{"offset":1}`, `[1]`, `{"type":"read","offset":"far"}`, `{"type":`, `{"type":1}`,
		// A field of another version of the protocol, at the top or deeper.
		`{"type":"queued","ahead":1,"behind":2}`,
		`{"type":"start","label":"a.mov","size":10,"output":{"format":"ipod","video":null,"audio":{"codec":"aac","bitrate":0,"channels":1},"max_seconds":0}}`,
	} {
		if m, err := Decode([]byte(b)); err == nil {
			t.Errorf("%s decoded as %+v", b, m)
		}
	}
}

func TestAllowFrom(t *testing.T) {
	prefixes := []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("2001:db8::/32")}
	handler := AllowFrom(prefixes, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for remote, want := range map[string]int{
		"192.0.2.7:1234":                http.StatusOK,
		"[::ffff:192.0.2.7]:1234":       http.StatusOK,
		"[2001:db8::1]:1234":            http.StatusOK,
		"[fe80::1%en0]:1234":            http.StatusForbidden,
		"198.51.100.7:1234":             http.StatusForbidden,
		"[2001:db9::1]:1234":            http.StatusForbidden,
		"not an address":                http.StatusForbidden,
		"192.0.2.7":                     http.StatusForbidden,
		"203.0.113.9:1234, 192.0.2.7:1": http.StatusForbidden,
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		r.Header.Set("X-Forwarded-For", "192.0.2.7")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%s: %d, want %d", remote, w.Code, want)
		}
		if want == http.StatusForbidden && !strings.Contains(w.Body.String(), "Forbidden") {
			t.Errorf("%s: answered %q", remote, w.Body.String())
		}
	}
}
