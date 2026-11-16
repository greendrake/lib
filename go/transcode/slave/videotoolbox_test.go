//go:build darwin

package slave_test

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"greendrake.nz/go/transcode"
	"greendrake.nz/go/transcode/internal/transcodetest"
	"greendrake.nz/go/transcode/slave"
)

// TestVideoToolbox checks, on a Mac, a slave configured as the README configures one: VideoToolbox decoding, falling
// back to software for a source it does not decode, its frames rotated, scaled and tone-mapped, and VideoToolbox's
// encoders with libx264's GOP — a key frame every 250 frames, B-frames — rather than ffmpeg's generic default of a key frame every
// 12 and none.
func TestVideoToolbox(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1, HWAccel: "videotoolbox", Encoders: map[string]slave.Encoder{
		"h264": {Name: "h264_videotoolbox", Args: []string{"-profile:v", "high", "-g", "250", "-bf", "3"}},
		"hevc": {Name: "hevc_videotoolbox", Args: []string{"-g", "250", "-bf", "3"}},
	}})
	// pattern is ffmpeg's arguments making a 4-second pattern of w×h with args.
	pattern := func(w, h int, args ...string) []string { return slices.Concat(transcodetest.Pattern(w, h, 4), args) }
	tags := map[string]string{"h264": "avc1", "hevc": "hvc1"}
	clip := transcodetest.MakeFile(t, "clip.mp4", pattern(640, 360, "-c:v", "libx264")...)
	for name, c := range map[string]struct {
		src     []byte
		size    [2]int // the output's, of a short side of 240
		toneMap bool
	}{
		"H.264, rotated":           {transcodetest.Make(t, "rotated.mp4", "-display_rotation", "90", "-i", clip, "-c", "copy"), [2]int{240, 426}, false},
		"HEVC, 10-bit HLG, to SDR": {transcodetest.Make(t, "hlg.mov", pattern(640, 360, transcodetest.HDR("arib-std-b67")...)...), [2]int{426, 240}, true},
		"MPEG-4 Part 2, declined":  {transcodetest.Make(t, "mpeg4.mp4", pattern(640, 360, "-c:v", "mpeg4")...), [2]int{426, 240}, false},
		"FFV1, odd-sized":          {transcodetest.Make(t, "odd.mkv", pattern(641, 359, "-c:v", "ffv1")...), [2]int{428, 240}, false},
	} {
		for _, codec := range []string{"h264", "hevc"} {
			out, err := transcodetest.Run(t, s, c.src, transcode.Output{Format: "mp4", Video: &transcode.Video{Codec: codec, MaxShortSide: 240, Bitrate: 500_000, PixelFormat: "yuv420p", ToneMap: c.toneMap}})
			if err != nil {
				t.Fatalf("%s to %s: %v", name, codec, err)
			}
			video := transcodetest.Probe(t, out).Streams[0]
			if video.Codec != codec || video.Tag != tags[codec] || [2]int{video.Width, video.Height} != c.size || video.PixelFormat != "yuv420p" || len(video.SideData) != 0 || (c.toneMap && video.Transfer != "bt709") {
				t.Errorf("%s to %s: %+v", name, codec, video)
			}
			frames, err := exec.Command(transcodetest.Tool(t, "ffprobe"), "-v", "error", "-select_streams", "v:0", "-show_entries", "frame=pict_type", "-of", "csv=p=0", transcodetest.Write(t, "output.mp4", out)).Output()
			if err != nil {
				t.Fatal(err)
			}
			if types := strings.Fields(string(frames)); strings.Count(string(frames), "I") != 1 || !slices.Contains(types, "B") {
				t.Errorf("%s to %s: frames %s", name, codec, strings.Join(types, ""))
			}
		}
	}
}
