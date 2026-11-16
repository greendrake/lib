package transcode

import (
	"fmt"
	"regexp"
	"strings"
)

// Output is what to make of a source, never how: the container and the codecs, as any ffmpeg names them; the slave
// chooses the encoder making each codec on its machine. One source in, one output out: video with or without audio,
// or audio alone. Every slave makes it the same way:
//
//   - Format: the muxer of that name writes the output, as one file. The MOV family (mp4, mov, ipod, 3gp, …) is
//     written with its index first (faststart), and HEVC in it tagged hvc1, as Apple's players require. A format
//     writing more than one file, a segmenting muxer, fails the job, and so does a device.
//   - Streams: the source's first video stream, where Video is set, and its first audio stream, where Audio is. The
//     output's leading stream — video, or audio for an audio-only output — is required of the source; audio beside
//     video is taken where the source has it. Nothing else: no subtitles, data streams, chapters, or cover art.
//   - Length: the source's whole, or its first MaxSeconds seconds.
//   - Orientation: as the source shows, the rotation in the pixels; the output carries none of its own.
//   - Size: both sides even, whatever the source's; the short side bounded by Video.MaxShortSide, never grown, the
//     long side keeping the aspect.
//   - Dynamic range: the source's, its colour tags carried over — unless Video.ToneMap, for an HDR source (HLG or
//     PQ), which is made SDR (BT.709) by tone mapping, in yuv420p unless the pixel format says otherwise. Only the
//     master knows a source is HDR (ffprobe's color_transfer arib-std-b67 or smpte2084); tone mapping an SDR source
//     darkens it, or fails the job where the source carries no colour tags.
//   - Codecs: encoded by the slave's encoder for each, at the bitrate in its average-bitrate mode where one is given,
//     in the pixel format where one is given.
//
// Whether ffmpeg has the muxer, or the pixel format for the encoder, is ffmpeg's to say: a job it refuses is a
// *Failure, ffmpeg's stderr its Detail.
type Output struct {
	Format     string  `json:"format"`      // the container, as ffmpeg names its muxer: "mp4", "matroska", "webm", "ipod" (m4a), "mp3", "ogg", "flac", …
	Video      *Video  `json:"video"`       // nil: no video
	Audio      *Audio  `json:"audio"`       // nil: no audio
	MaxSeconds float64 `json:"max_seconds"` // 0: the whole source; else its first MaxSeconds seconds
}

// Video is the video stream of an Output.
type Video struct {
	Codec        string `json:"codec"`          // as ffmpeg names the codec: "h264", "hevc", "vp9", "av1", "prores", …
	MaxShortSide int    `json:"max_short_side"` // 0: the source's size; else, an even number, the short side scaled down to it, never up
	Bitrate      int    `json:"bitrate"`        // average bits per second; 0: the encoder's default rate control
	PixelFormat  string `json:"pixel_format"`   // as ffmpeg names it, e.g. "yuv420p"; empty: the encoder's default
	ToneMap      bool   `json:"tone_map"`       // the source is HDR, to be made SDR; false: the source's dynamic range
}

// Audio is the audio stream of an Output.
type Audio struct {
	Codec   string `json:"codec"`   // "aac", "opus", "mp3", "flac", …
	Bitrate int    `json:"bitrate"` // bits per second; 0: the encoder's default
}

// plainName is a name of ffmpeg's — a muxer's, a codec's, a pixel format's — as an Output gives one: a name, never an
// option.
var plainName = regexp.MustCompile(`^[a-z0-9_]+$`)

// Validate answers a *Failure for an output no slave makes: one with neither video nor audio, a format, codec or
// pixel format that is not a plain name (lowercase letters, digits and underscores), a negative or odd MaxShortSide,
// a negative bitrate, or a negative MaxSeconds.
func (o Output) Validate() error {
	if o.Video == nil && o.Audio == nil {
		return &Failure{Message: "transcode: the output has neither video nor audio"}
	}
	var problems []string
	name := func(what, name string) {
		if !plainName.MatchString(name) {
			problems = append(problems, fmt.Sprintf("%s %q is not a plain name", what, name))
		}
	}
	bitrate := func(what string, bitrate int) {
		if bitrate < 0 {
			problems = append(problems, fmt.Sprintf("%s bitrate %d is negative", what, bitrate))
		}
	}
	name("format", o.Format)
	if o.MaxSeconds < 0 {
		problems = append(problems, fmt.Sprintf("max seconds %g is negative", o.MaxSeconds))
	}
	if v := o.Video; v != nil {
		name("video codec", v.Codec)
		if v.PixelFormat != "" {
			name("pixel format", v.PixelFormat)
		}
		if v.MaxShortSide < 0 || v.MaxShortSide%2 != 0 {
			problems = append(problems, fmt.Sprintf("max short side %d is neither 0 nor an even number from 2 up", v.MaxShortSide))
		}
		bitrate("video", v.Bitrate)
	}
	if a := o.Audio; a != nil {
		name("audio codec", a.Codec)
		bitrate("audio", a.Bitrate)
	}
	if len(problems) > 0 {
		return &Failure{Message: "transcode: invalid output: " + strings.Join(problems, "; ")}
	}
	return nil
}
