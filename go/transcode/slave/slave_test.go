package slave_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"greendrake.nz/go/transcode"
	"greendrake.nz/go/transcode/internal/transcodetest"
	"greendrake.nz/go/transcode/slave"
)

func TestNew(t *testing.T) {
	ffmpeg := transcodetest.Tool(t, "ffmpeg")
	spool := t.TempDir()
	encoders := func(codec, name string) map[string]slave.Encoder {
		return map[string]slave.Encoder{codec: {Name: name}}
	}
	for name, c := range map[string]struct {
		cfg  slave.Config
		want string // in the refusal
	}{
		"no jobs":                 {slave.Config{FFmpeg: ffmpeg, Spool: spool}, "Jobs is 0"},
		"no spool":                {slave.Config{FFmpeg: ffmpeg, Jobs: 1}, "no Spool"},
		"a spool that is not":     {slave.Config{FFmpeg: ffmpeg, Spool: filepath.Join(spool, "none", "deeper"), Jobs: 1}, "no such file"},
		"an unnamed ffmpeg":       {slave.Config{Spool: spool, Jobs: 1}, "no FFmpeg"},
		"no ffmpeg":               {slave.Config{FFmpeg: filepath.Join(spool, "ffmpeg"), Spool: spool, Jobs: 1}, "ffmpeg -version"},
		"an encoder as a codec":   {slave.Config{FFmpeg: ffmpeg, Spool: spool, Jobs: 1, Encoders: encoders("libx264", "libx264")}, `"libx264" is no codec`},
		"another codec's encoder": {slave.Config{FFmpeg: ffmpeg, Spool: spool, Jobs: 1, Encoders: encoders("h264", "libx265")}, `not "libx265"`},
		"no such encoder":         {slave.Config{FFmpeg: ffmpeg, Spool: spool, Jobs: 1, Encoders: encoders("h264", "h264_nonesuch")}, `not "h264_nonesuch"`},
		"no such hwaccel":         {slave.Config{FFmpeg: ffmpeg, Spool: spool, Jobs: 1, HWAccel: "nonesuch"}, `has no "nonesuch"`},
	} {
		s, err := slave.New(c.cfg)
		if err == nil {
			s.Close()
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	if entries, err := os.ReadDir(spool); err != nil || len(entries) != 0 {
		t.Errorf("the spool holds %v, %v", entries, err)
	}
}

func TestSpoolShared(t *testing.T) {
	spool := t.TempDir()
	theirs := []string{"theirs.mp4", "transcode-theirs"}
	if err := os.WriteFile(filepath.Join(spool, theirs[0]), []byte("theirs"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(spool, theirs[1]), 0o700); err != nil {
		t.Fatal(err)
	}
	s, err := slave.New(slave.Config{FFmpeg: transcodetest.Tool(t, "ffmpeg"), Spool: spool, Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transcodetest.Run(t, s, transcodetest.Clip(t, 160, 120), transcodetest.AVC); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(spool)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, theirs) {
		t.Errorf("the spool holds %v, want %v", names, theirs)
	}
}

func TestOutput(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	subtitles := transcodetest.Write(t, "subs.srt", []byte("1\n00:00:00,000 --> 00:00:01,000\nhello\n"))
	chapters := transcodetest.Write(t, "chapters.txt", []byte(";FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=1000\ntitle=one\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=1000\nEND=2000\ntitle=two\n"))
	busy := transcodetest.MakeFile(t, "busy.mkv", slices.Concat(transcodetest.Pattern(320, 240, 2), transcodetest.Tone(2), []string{
		"-f", "lavfi", "-i", "sine=frequency=880:duration=2", "-i", subtitles, "-i", chapters,
		"-map", "0", "-map", "1", "-map", "2", "-map", "3", "-map_chapters", "4", "-c:v", "libx264", "-pix_fmt", "yuv444p", "-c:a", "aac", "-c:s", "srt",
	})...)
	// As a phone writes it: rotated, its index at the end.
	src := transcodetest.Make(t, "busy.mp4", "-display_rotation", "90", "-i", busy, "-map", "0", "-c", "copy", "-c:s", "mov_text")
	if m := transcodetest.Probe(t, src); !strings.HasPrefix(m.Kinds(), "video,audio,audio,subtitle") || len(m.Chapters) != 2 || len(m.Streams[0].SideData) != 1 {
		t.Fatalf("the source has %s, %d chapters, side data %+v", m.Kinds(), len(m.Chapters), m.Streams[0].SideData)
	}

	const bitrate = 200_000
	o := transcode.Output{Format: "mp4", Video: &transcode.Video{Codec: "h264", MaxShortSide: 120, Bitrate: bitrate, PixelFormat: "yuv420p"}, Audio: &transcode.Audio{Codec: "aac", Bitrate: 64_000}}
	out, err := transcodetest.Run(t, s, src, o)
	if err != nil {
		t.Fatal(err)
	}
	m := transcodetest.Probe(t, out)
	if m.Kinds() != "video,audio" || len(m.Chapters) != 0 {
		t.Fatalf("the output has %s and %d chapters", m.Kinds(), len(m.Chapters))
	}
	video, audio := m.Streams[0], m.Streams[1]
	if video.Codec != "h264" || video.Width != 120 || video.Height != 160 || video.PixelFormat != "yuv420p" || len(video.SideData) != 0 {
		t.Errorf("the video is %+v", video)
	}
	if rate, err := strconv.Atoi(video.BitRate); err != nil || rate < bitrate/2 || rate > bitrate*6/5 {
		t.Errorf("the video's bitrate is %s, asked %d", video.BitRate, bitrate)
	}
	if audio.Codec != "aac" {
		t.Errorf("the audio is %+v", audio)
	}
	if seconds := m.Seconds(t); seconds < 1.9 || seconds > 2.1 {
		t.Errorf("%g seconds", seconds)
	}
	if boxes := transcodetest.Boxes(t, out); slices.Index(boxes, "moov") > slices.Index(boxes, "mdat") {
		t.Errorf("the boxes are %v: the index after the media", boxes)
	}

	// Never grown.
	o.Video.MaxShortSide = 480
	if out, err = transcodetest.Run(t, s, src, o); err != nil {
		t.Fatal(err)
	}
	if video := transcodetest.Probe(t, out).Streams[0]; video.Width != 240 || video.Height != 320 {
		t.Errorf("the video is %dx%d, want the source's 240x320", video.Width, video.Height)
	}
}

func TestEvenSize(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	src := transcodetest.Make(t, "odd.mkv", slices.Concat(transcodetest.Pattern(641, 359, 1), []string{"-c:v", "ffv1"})...)
	if video := transcodetest.Probe(t, src).Streams[0]; video.Width != 641 || video.Height != 359 {
		t.Fatalf("the source is %dx%d", video.Width, video.Height)
	}
	// The long side keeps the aspect to the nearest even number: 641·200/359 ≈ 357.1 makes 358.
	for short, want := range map[int][2]int{0: {640, 358}, 200: {358, 200}, 400: {640, 358}} {
		out, err := transcodetest.Run(t, s, src, transcode.Output{Format: "mp4", Video: &transcode.Video{Codec: "h264", MaxShortSide: short}})
		if err != nil {
			t.Fatalf("short side %d: %v", short, err)
		}
		if video := transcodetest.Probe(t, out).Streams[0]; [2]int{video.Width, video.Height} != want {
			t.Errorf("short side %d: %dx%d, want %dx%d", short, video.Width, video.Height, want[0], want[1])
		}
	}
}

func TestFormats(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	out, err := transcodetest.Run(t, s, transcodetest.Clip(t, 320, 240), transcode.Output{Format: "matroska", Video: &transcode.Video{Codec: "mpeg4"}, Audio: &transcode.Audio{Codec: "flac"}})
	if err != nil {
		t.Fatal(err)
	}
	m := transcodetest.Probe(t, out)
	if !strings.HasPrefix(m.Format.Name, "matroska") || m.Kinds() != "video,audio" || m.Streams[0].Codec != "mpeg4" || m.Streams[1].Codec != "flac" {
		t.Errorf("a %s of %+v", m.Format.Name, m.Streams)
	}
}

func TestHEVCTag(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	src := transcodetest.Clip(t, 160, 120)
	for _, c := range []struct{ format, codec, tag string }{
		{"mp4", "hevc", "hvc1"},
		{"mov", "hevc", "hvc1"},
		{"mp4", "h264", "avc1"},
	} {
		out, err := transcodetest.Run(t, s, src, transcode.Output{Format: c.format, Video: &transcode.Video{Codec: c.codec}})
		if err != nil {
			t.Fatalf("%s in %s: %v", c.codec, c.format, err)
		}
		if video := transcodetest.Probe(t, out).Streams[0]; video.Tag != c.tag {
			t.Errorf("%s in %s: tagged %s, want %s", c.codec, c.format, video.Tag, c.tag)
		}
	}
}

func TestToneMap(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	hdr := func(name, transfer string) []byte {
		return transcodetest.Make(t, name, slices.Concat(transcodetest.Pattern(320, 240, 1), transcodetest.HDR(transfer))...)
	}
	hlg, pq := hdr("hlg.mp4", "arib-std-b67"), hdr("pq.mp4", "smpte2084")
	type colour struct{ pixels, transfer, primaries, matrix string }
	sdr := colour{"yuv420p", "bt709", "bt709", "bt709"}
	for name, c := range map[string]struct {
		src   []byte
		video transcode.Video
		want  colour
	}{
		"HLG kept":          {hlg, transcode.Video{Codec: "hevc", PixelFormat: "yuv420p10le"}, colour{"yuv420p10le", "arib-std-b67", "bt2020", "bt2020nc"}},
		"HLG tone-mapped":   {hlg, transcode.Video{Codec: "h264", ToneMap: true}, sdr},
		"PQ tone-mapped":    {pq, transcode.Video{Codec: "h264", ToneMap: true}, sdr},
		"to 10-bit SDR":     {hlg, transcode.Video{Codec: "hevc", PixelFormat: "yuv420p10le", ToneMap: true}, colour{"yuv420p10le", "bt709", "bt709", "bt709"}},
		"scaled and mapped": {pq, transcode.Video{Codec: "h264", MaxShortSide: 120, ToneMap: true}, sdr},
	} {
		out, err := transcodetest.Run(t, s, c.src, transcode.Output{Format: "mp4", Video: &c.video})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		video := transcodetest.Probe(t, out).Streams[0]
		if got := (colour{video.PixelFormat, video.Transfer, video.Primaries, video.Matrix}); got != c.want {
			t.Errorf("%s: %+v, want %+v", name, got, c.want)
		}
	}

	// A source without colour tags is none to tone-map: the master asked for what it cannot be.
	_, err := transcodetest.Run(t, s, transcodetest.Clip(t, 160, 120), transcode.Output{Format: "mp4", Video: &transcode.Video{Codec: "h264", ToneMap: true}})
	var failure *transcode.Failure
	if !errors.As(err, &failure) || failure.Detail == "" {
		t.Errorf("an untagged source tone-mapped: %v", err)
	}
}

func TestAudioAlone(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	cover := transcodetest.MakeFile(t, "cover.png", slices.Concat(transcodetest.Pattern(64, 64, 1), []string{"-frames:v", "1"})...)
	song := transcodetest.Make(t, "song.mp3", slices.Concat(transcodetest.Tone(2), []string{
		"-i", cover, "-map", "0", "-map", "1", "-c:a", "libmp3lame", "-c:v", "copy", "-disposition:v", "attached_pic",
	})...)
	if kinds := transcodetest.Probe(t, song).Kinds(); kinds != "audio,video" {
		t.Fatalf("the song has %s", kinds)
	}
	for name, src := range map[string][]byte{"a song with its cover": song, "a video's sound": transcodetest.Clip(t, 160, 120)} {
		out, err := transcodetest.Run(t, s, src, transcode.Output{Format: "ipod", Audio: &transcode.Audio{Codec: "aac", Bitrate: 96_000}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if m := transcodetest.Probe(t, out); m.Kinds() != "audio" || m.Streams[0].Codec != "aac" {
			t.Errorf("%s: %+v", name, m.Streams)
		}
	}
}

func TestSilentVideo(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	out, err := transcodetest.Run(t, s, transcodetest.Silent(t, 160, 120), transcodetest.AVC)
	if err != nil {
		t.Fatal(err)
	}
	if kinds := transcodetest.Probe(t, out).Kinds(); kinds != "video" {
		t.Errorf("the output has %s", kinds)
	}
}

// MaxSeconds keeps the source's first seconds, of every stream.
func TestMaxSeconds(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	o := transcodetest.AVC
	o.MaxSeconds = 0.5
	out, err := transcodetest.Run(t, s, transcodetest.Clip(t, 160, 120), o)
	if err != nil {
		t.Fatal(err)
	}
	m := transcodetest.Probe(t, out)
	if m.Kinds() != "video,audio" {
		t.Errorf("the output has %s", m.Kinds())
	}
	if seconds := m.Seconds(t); seconds < 0.45 || seconds > 0.6 {
		t.Errorf("%g seconds of a two-second source, want 0.5", seconds)
	}
}

// A video the hardware acceleration fails to decode is decoded again in software, and one software decoding fails too
// fails; an output without video is decoded in software at once. The hardware here is an ffmpeg failing every run
// with -hwaccel, the acceleration one the ffmpeg on PATH has; it notes each job's run as hw or sw.
func TestSoftwareDecoding(t *testing.T) {
	ffmpeg := transcodetest.Tool(t, "ffmpeg")
	out, err := exec.Command(ffmpeg, "-hide_banner", "-hwaccels").Output()
	if err != nil {
		t.Fatal(err)
	}
	_, list, _ := strings.Cut(string(out), "\n")
	methods := strings.Fields(list)
	if len(methods) == 0 {
		t.Fatal("the test needs an ffmpeg with a hardware acceleration")
	}
	dir := t.TempDir()
	runs, wrapper := filepath.Join(dir, "runs"), filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\nfor a; do\n  if [ \"$a\" = -hwaccel ]; then echo hw >> '" + runs + "'; echo 'hardware decoding failed' >&2; exit 1; fi\ndone\n" +
		"case \"$*\" in *-progress*) echo sw >> '" + runs + "';; esac\nexec '" + ffmpeg + "' \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := transcodetest.NewSlave(t, slave.Config{FFmpeg: wrapper, Jobs: 1, HWAccel: methods[0]})
	// ran is the runs of the job done, in order.
	ran := func() string {
		t.Helper()
		b, err := os.ReadFile(runs)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(runs); err != nil {
			t.Fatal(err)
		}
		return strings.Join(strings.Fields(string(b)), ",")
	}
	clip := transcodetest.Clip(t, 160, 120)
	if _, err := transcodetest.Run(t, s, clip, transcodetest.AVC); err != nil {
		t.Fatal(err)
	}
	if runs, outcome := ran(), s.Outcome(); runs != "hw,sw" || outcome != "ok" {
		t.Errorf("a video: runs %s, %s", runs, outcome)
	}
	if _, err := transcodetest.Run(t, s, clip, transcode.Output{Format: "ipod", Audio: &transcode.Audio{Codec: "aac"}}); err != nil {
		t.Fatal(err)
	}
	if runs, outcome := ran(), s.Outcome(); runs != "sw" || outcome != "ok" {
		t.Errorf("audio alone: runs %s, %s", runs, outcome)
	}
	var failure *transcode.Failure
	if _, err := transcodetest.Run(t, s, []byte("not media at all"), transcodetest.AVC); !errors.As(err, &failure) {
		t.Errorf("not media: %v", err)
	}
	if runs, outcome := ran(), s.Outcome(); runs != "hw,sw" || !strings.HasPrefix(outcome, "failed: ") {
		t.Errorf("not media: runs %s, %s", runs, outcome)
	}
}

func TestEncoder(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1, Encoders: map[string]slave.Encoder{"h264": {Name: "libx264", Args: []string{"-preset", "ultrafast", "-profile:v", "baseline"}}}})
	out, err := transcodetest.Run(t, s, transcodetest.Clip(t, 320, 240), transcodetest.AVC)
	if err != nil {
		t.Fatal(err)
	}
	if video := transcodetest.Probe(t, out).Streams[0]; video.Codec != "h264" || video.Profile != "Constrained Baseline" {
		t.Errorf("the video is %+v", video)
	}
	if h := s.Health(); !strings.HasPrefix(h.FFmpeg, "ffmpeg version ") || h.Encoders["h264"].Name != "libx264" || h.Running != 0 || h.Waiting != 0 {
		t.Errorf("health %+v", h)
	}
}

// outputDevice is an output device the ffmpeg on PATH has: the first of the names of output devices it knows as a
// muxer, which says nothing of `ffmpeg -devices`, where the slave learns them.
func outputDevice(t *testing.T) string {
	for _, name := range []string{"audiotoolbox", "alsa", "pulse", "oss", "sdl", "xv", "fbdev", "v4l2", "opengl", "caca"} {
		out, err := exec.Command(transcodetest.Tool(t, "ffmpeg"), "-hide_banner", "-h", "muxer="+name).Output()
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(string(out), "Muxer "+name+" ") {
			return name
		}
	}
	t.Fatal("ffmpeg has none of the output devices known")
	return ""
}

func TestFailures(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	video := transcodetest.Clip(t, 160, 120)
	silent := transcodetest.Silent(t, 160, 120)
	for name, c := range map[string]struct {
		src    []byte
		output transcode.Output
		want   string // in the message
		detail bool
	}{
		"an invalid output":         {video, transcode.Output{Format: "mp4"}, "neither video nor audio", false},
		"an unreadable source":      {bytes.Repeat([]byte("not media "), 10000), transcodetest.AVC, "ffmpeg could not make the output", true},
		"audio a source lacks":      {silent, transcode.Output{Format: "ipod", Audio: &transcode.Audio{Codec: "aac"}}, "ffmpeg could not make the output", true},
		"a codec without encoder":   {video, transcode.Output{Format: "matroska", Video: &transcode.Video{Codec: "vp6"}}, `"vp6" is no video codec`, false},
		"an encoder as a codec":     {video, transcode.Output{Format: "mp4", Video: &transcode.Video{Codec: "libx264"}}, `"libx264" is no video codec`, false},
		"an audio codec as video":   {video, transcode.Output{Format: "mp4", Video: &transcode.Video{Codec: "aac"}}, `"aac" is no video codec`, false},
		"a video codec as audio":    {video, transcode.Output{Format: "ipod", Audio: &transcode.Audio{Codec: "h264"}}, `"h264" is no audio codec`, false},
		"no such muxer":             {video, transcode.Output{Format: "nonesuch", Audio: &transcode.Audio{Codec: "aac"}}, "ffmpeg could not make the output", true},
		"a segmenting format":       {video, transcode.Output{Format: "hls", Video: &transcode.Video{Codec: "h264"}}, `format "hls" writes more than one file`, false},
		"a format writing no file":  {video, transcode.Output{Format: "null", Audio: &transcode.Audio{Codec: "aac"}}, `format "null" wrote no file`, false},
		"a device":                  {video, transcode.Output{Format: outputDevice(t), Audio: &transcode.Audio{Codec: "pcm_s16le"}}, "is a device", false},
		"an input device":           {video, transcode.Output{Format: "lavfi", Audio: &transcode.Audio{Codec: "aac"}}, "ffmpeg could not make the output", true},
		"no such pixel format":      {video, transcode.Output{Format: "mp4", Video: &transcode.Video{Codec: "h264", PixelFormat: "nonesuch"}}, "ffmpeg could not make the output", true},
		"a muxer refusing a codec":  {video, transcode.Output{Format: "mp3", Audio: &transcode.Audio{Codec: "flac"}}, "ffmpeg could not make the output", true},
		"an empty source":           {nil, transcodetest.AVC, "ffmpeg could not make the output", true},
		"a negative bitrate":        {video, transcode.Output{Format: "ipod", Audio: &transcode.Audio{Codec: "aac", Bitrate: -1}}, "negative", false},
		"an option as a format":     {video, transcode.Output{Format: "mp4 -y", Audio: &transcode.Audio{Codec: "aac"}}, "not a plain name", false},
		"an odd short side":         {video, transcode.Output{Format: "mp4", Video: &transcode.Video{Codec: "h264", MaxShortSide: 101}}, "max short side", false},
		"a pixel format as options": {video, transcode.Output{Format: "mp4", Video: &transcode.Video{Codec: "h264", PixelFormat: "-vf"}}, "not a plain name", false},
	} {
		_, err := transcodetest.Run(t, s, c.src, c.output)
		var failure *transcode.Failure
		switch {
		case !errors.As(err, &failure):
			t.Errorf("%s: %v, no Failure", name, err)
		case !strings.Contains(failure.Message, c.want) || (failure.Detail != "") != c.detail:
			t.Errorf("%s: %q, detail %q", name, failure.Message, failure.Detail)
		}
		if outcome := s.Outcome(); !strings.HasPrefix(outcome, "failed: ") {
			t.Errorf("%s: logged %q", name, outcome)
		}
		if jobs := s.Jobs(t); len(jobs) != 0 {
			t.Errorf("%s: left %v", name, jobs)
		}
	}
}

func TestSourceErrors(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	src := transcodetest.Clip(t, 320, 240)
	for name, job := range map[string]transcode.Job{
		"a read failing": {Source: transcodetest.Failing{ReaderAt: bytes.NewReader(src), At: int64(len(src) / 2)}, Size: int64(len(src)), Output: transcodetest.AVC},
		"a short source": {Source: bytes.NewReader(src[:len(src)/2]), Size: int64(len(src)), Output: transcodetest.AVC},
	} {
		_, err := s.Transcode(context.Background(), job, &bytes.Buffer{})
		var failure *transcode.Failure
		if err == nil || errors.As(err, &failure) || !strings.Contains(err.Error(), "transcode: reading the source: ") {
			t.Errorf("%s: %v", name, err)
		}
		if outcome := s.Outcome(); !strings.HasPrefix(outcome, "failed: transcode: reading the source: ") {
			t.Errorf("%s: logged %q", name, outcome)
		}
	}
}

func TestHardening(t *testing.T) {
	var asked atomic.Int64
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		http.NotFound(w, r)
	}))
	defer elsewhere.Close()
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	for name, playlist := range map[string]string{
		"HLS":    "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXTINF:2.0,\n" + elsewhere.URL + "/clip.ts\n#EXT-X-ENDLIST\n",
		"concat": "ffconcat version 1.0\nfile '" + elsewhere.URL + "/clip.mp4'\n",
		"a path": "ffconcat version 1.0\nfile '/etc/hosts'\n",
	} {
		_, err := transcodetest.Run(t, s, []byte(playlist), transcodetest.AVC)
		if _, ok := errors.AsType[*transcode.Failure](err); !ok {
			t.Errorf("%s: %v", name, err)
		}
		s.Outcome()
	}
	if n := asked.Load(); n != 0 {
		t.Errorf("a playlist had %d requests made", n)
	}
}
