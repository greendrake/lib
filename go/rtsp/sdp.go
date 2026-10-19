package rtsp

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Description is what a server describes a stream as (its SDP, RFC 4566): as far as
// playing one of its media goes.
type Description struct {
	// Where the stream as a whole is played.
	Control string
	Medias  []Media
}

// Video is the stream's first video media.
func (d *Description) Video() (Media, bool) {
	for _, media := range d.Medias {
		if media.Type == "video" {
			return media, true
		}
	}
	return Media{}, false
}

// Media is one of a stream's media, by its first format.
type Media struct {
	// "video" or "audio".
	Type string
	// Where the media is set up.
	Control     string
	PayloadType uint8
	// The format's encoding name, as rtpmap gives it: "H265".
	Encoding  string
	ClockRate int
	// The format's parameters, as fmtp gives them.
	Params map[string]string
	// Frames a second, of a video whose description gives them; 0 otherwise.
	FrameRate float64
}

// staticFormats are the payload types RFC 3551 gives a format of their own, which a
// media need not map: those cameras send.
var staticFormats = map[uint8]struct {
	encoding string
	rate     int
}{
	0:  {"PCMU", 8000},
	8:  {"PCMA", 8000},
	26: {"JPEG", 90000},
}

// frameRates are the attributes that give a media's frame rate, by name, each with where
// in its value the rate is: RFC 4566's "framerate:<rate>", and BITVISION's
// "videoinfo:<width>*<height>*<rate>*<bit rate>".
var frameRates = map[string]func(value string) string{
	"framerate": func(value string) string { return value },
	"videoinfo": func(value string) string {
		fields := strings.Split(value, "*")
		if len(fields) != 4 {
			return ""
		}
		return fields[2]
	},
}

// parseDescription reads an SDP description, its controls resolved against base.
func parseDescription(sdp []byte, base string) (*Description, error) {
	d := &Description{Control: base}
	var media *Media
	scanner := bufio.NewScanner(bytes.NewReader(sdp))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		kind, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		// An attribute's name and value, of an "a" line.
		attribute, attributeValue, _ := strings.Cut(value, ":")
		switch {
		case kind == "m":
			// m=<type> <port> <proto> <payload type>...
			fields := strings.Fields(value)
			if len(fields) < 4 {
				return nil, fmt.Errorf("rtsp: media line %q", line)
			}
			pt, err := strconv.ParseUint(fields[3], 10, 7)
			if err != nil {
				return nil, fmt.Errorf("rtsp: media line %q", line)
			}
			d.Medias = append(d.Medias, Media{Type: fields[0], Control: base, PayloadType: uint8(pt), Params: map[string]string{}})
			media = &d.Medias[len(d.Medias)-1]
			if static, known := staticFormats[media.PayloadType]; known {
				media.Encoding, media.ClockRate = static.encoding, static.rate
			}
		case kind == "a" && attribute == "control":
			control := resolve(base, attributeValue)
			if media == nil {
				d.Control = control
			} else {
				media.Control = control
			}
		case kind == "a" && media != nil && attribute == "rtpmap":
			// rtpmap:<payload type> <encoding>/<clock rate>[/<channels>]
			pt, format, _ := strings.Cut(attributeValue, " ")
			if pt != strconv.Itoa(int(media.PayloadType)) {
				continue
			}
			encoding, rest, _ := strings.Cut(format, "/")
			clock, _, _ := strings.Cut(rest, "/")
			rate, err := strconv.Atoi(clock)
			if err != nil {
				return nil, fmt.Errorf("rtsp: rtpmap %q", line)
			}
			media.Encoding, media.ClockRate = strings.ToUpper(encoding), rate
		case kind == "a" && media != nil && attribute == "fmtp":
			// fmtp:<payload type> <key>=<value>;...
			pt, params, _ := strings.Cut(attributeValue, " ")
			if pt != strconv.Itoa(int(media.PayloadType)) {
				continue
			}
			for _, param := range strings.Split(params, ";") {
				key, value, _ := strings.Cut(strings.TrimSpace(param), "=")
				media.Params[key] = value
			}
		case kind == "a" && media != nil && frameRates[attribute] != nil:
			rate, err := strconv.ParseFloat(frameRates[attribute](attributeValue), 64)
			if err != nil || rate <= 0 {
				return nil, fmt.Errorf("rtsp: frame rate %q", line)
			}
			media.FrameRate = rate
		}
	}
	return d, scanner.Err()
}

// resolve is the URL control names: itself where absolute, base for "*", and relative to
// base otherwise.
func resolve(base, control string) string {
	switch {
	case strings.HasPrefix(control, "rtsp://"):
		return control
	case control == "*":
		return base
	case strings.HasSuffix(base, "/"):
		return base + control
	}
	return base + "/" + control
}
