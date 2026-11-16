package slave

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"greendrake.nz/go/transcode"
)

var (
	// codecLine is a line of `ffmpeg -codecs` of a codec it encodes: its kind (V video, A audio, S subtitle, D data,
	// T attachment), its name, and its encoders where they are not just the one named as it.
	codecLine = regexp.MustCompile(`(?m)^ [D.]E([VASDT])[I.][L.][S.] (\S+) .*?(?:\(encoders: ([^)]*)\))?$`)
	// deviceLine is a line of `ffmpeg -devices` of an output device: its names.
	deviceLine = regexp.MustCompile(`(?m)^ [D ]E (\S+)`)
)

// demuxers are the input formats a source may be in, as ffmpeg names them: containers and plain streams, none of
// which opens anything beside its own input, as a playlist (HLS, concat) would the URLs and paths in it. The README
// lists them for masters, which have nothing else to go by.
var demuxers = strings.Join([]string{"mov", "matroska", "avi", "flv", "mpegts", "mpeg", "ogg", "asf", "wav", "aiff", "caf", "mp3", "flac", "aac", "ac3", "eac3", "amr", "wv"}, ",")

// movFamily are the muxers of the MOV family, which write the index first with faststart, and HEVC tagged hvc1 —
// ffmpeg's default hev1 is HEVC that Apple's players do not play.
var movFamily = map[string]bool{"mp4": true, "mov": true, "ipod": true, "3gp": true, "3g2": true, "psp": true, "f4v": true}

// ToneMapping is the ffmpeg filter chain making SDR (BT.709) of an HDR source's frames, in the pixel format: to linear
// light, to BT.709's primaries, the highlights compressed (mobius, which leaves what SDR can show as it is), to BT.709's
// curve and matrix. zscale takes the source's transfer from its frames' colour tags. It is what Video.ToneMap has a
// slave apply; a program's own ffmpeg command applies it to show an HDR source — a still of it, say — as a slave's output
// shows it.
func ToneMapping(pixelFormat string) string {
	return "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=mobius:desat=0,zscale=t=bt709:m=bt709:r=tv,format=" + pixelFormat
}

// progressPeriod is how often ffmpeg reports its progress, in seconds.
const progressPeriod = "2"

// probe reads what s's ffmpeg is and has, and checks s's configuration against it.
func (s *Slave) probe() error {
	version, err := s.query("-version")
	if err != nil {
		return err
	}
	s.version, _, _ = strings.Cut(version, "\n")

	codecs, err := s.query("-codecs")
	if err != nil {
		return err
	}
	s.codecs = map[string]byte{}
	encoders := map[string][]string{} // by codec
	for _, m := range codecLine.FindAllStringSubmatch(codecs, -1) {
		s.codecs[m[2]] = m[1][0]
		encoders[m[2]] = strings.Fields(m[3])
		if m[3] == "" {
			encoders[m[2]] = []string{m[2]}
		}
	}
	for _, codec := range slices.Sorted(maps.Keys(s.encoders)) {
		name := s.encoders[codec].Name
		if _, ok := s.codecs[codec]; !ok {
			return fmt.Errorf("slave: Encoders: %q is no codec %s encodes", codec, s.ffmpeg)
		}
		if !slices.Contains(encoders[codec], name) {
			return fmt.Errorf("slave: Encoders: %s encodes %s with %s, not %q", s.ffmpeg, codec, strings.Join(encoders[codec], ", "), name)
		}
	}

	devices, err := s.query("-devices")
	if err != nil {
		return err
	}
	s.devices = map[string]bool{}
	for _, m := range deviceLine.FindAllStringSubmatch(devices, -1) {
		for name := range strings.SplitSeq(m[1], ",") {
			s.devices[name] = true
		}
	}

	if s.hwaccel == "" {
		return nil
	}
	hwaccels, err := s.query("-hwaccels")
	if err != nil {
		return err
	}
	if _, methods, _ := strings.Cut(hwaccels, "\n"); !slices.Contains(strings.Fields(methods), s.hwaccel) {
		return fmt.Errorf("slave: HWAccel: %s has no %q", s.ffmpeg, s.hwaccel)
	}
	return nil
}

// query is what ffmpeg prints for an option listing what it is or has (-version, -codecs, …).
func (s *Slave) query(option string) (string, error) {
	out, err := exec.Command(s.ffmpeg, "-hide_banner", option).Output()
	if err != nil {
		return "", fmt.Errorf("slave: %s %s: %w", s.ffmpeg, option, err)
	}
	return string(out), nil
}

// command is ffmpeg's arguments making o of the source at the URL input in the file output, decoding by the hwaccel
// (empty: in software): built from o alone, whose names are names, never arguments, paths or URLs. The input is
// confined to the loopback URL, the protocols it takes and the plain media containers; the output to the file
// protocol.
func (s *Slave) command(o transcode.Output, input, output, hwaccel string) []string {
	args := []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "error", "-progress", "pipe:1", "-stats_period", progressPeriod}
	if hwaccel != "" {
		args = append(args, "-hwaccel", hwaccel)
	}
	args = append(args, "-protocol_whitelist", "http,tcp", "-format_whitelist", demuxers, "-i", input)
	if v := o.Video; v != nil {
		filters := "scale='2*trunc(iw/2)':'2*trunc(ih/2)'"
		if v.MaxShortSide > 0 {
			filters = fmt.Sprintf("scale='if(gt(iw,ih),-2,2*trunc(min(%[1]d,iw)/2))':'if(gt(iw,ih),2*trunc(min(%[1]d,ih)/2),-2)'", v.MaxShortSide)
		}
		if v.ToneMap {
			// After the scaling, so that the tone mapping, in 32-bit floats, works on the output's pixels.
			filters += "," + ToneMapping(cmp.Or(v.PixelFormat, "yuv420p"))
		}
		args = append(append(args, "-map", "0:v:0", "-vf", filters), s.encoder("v", v.Codec, v.Bitrate)...)
		if v.PixelFormat != "" {
			args = append(args, "-pix_fmt", v.PixelFormat)
		}
		if v.ToneMap {
			args = append(args, "-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709")
		}
	}
	if a := o.Audio; a != nil {
		stream := "0:a:0"
		if o.Video != nil {
			stream += "?" // audio beside video, where the source has it
		}
		args = append(append(args, "-map", stream), s.encoder("a", a.Codec, a.Bitrate)...)
	}
	if o.MaxSeconds > 0 {
		args = append(args, "-t", strconv.FormatFloat(o.MaxSeconds, 'f', -1, 64))
	}
	args = append(args, "-map_chapters", "-1", "-f", o.Format)
	if movFamily[o.Format] {
		args = append(args, "-movflags", "+faststart")
		if o.Video != nil && o.Video.Codec == "hevc" {
			args = append(args, "-tag:v", "hvc1")
		}
	}
	return append(args, "-protocol_whitelist", "file", output)
}

// encoder is the arguments encoding the stream of a kind ("v", "a") in codec at bitrate: by the encoder configured
// for the codec, with its arguments, else by ffmpeg's default encoder for it.
func (s *Slave) encoder(kind, codec string, bitrate int) []string {
	args := []string{"-c:" + kind, codec}
	if enc, ok := s.encoders[codec]; ok {
		args = append([]string{"-c:" + kind, enc.Name}, enc.Args...)
	}
	if bitrate > 0 {
		args = append(args, "-b:"+kind, strconv.Itoa(bitrate))
	}
	return args
}

// run does t, its slot held: ffmpeg reads the source from a loopback input and writes the output to a directory of
// the job's own, from which, ffmpeg done, it goes to t.deliver. A source error ends the job through cancel.
//
// A video's source is decoded by the hardware acceleration configured, and one it fails is decoded again in software:
// a hardware decoder gives up on damage a software one conceals — after a broken frame in a long GOP, say, it loses
// every frame up to the next key frame, which may be the whole rest of the source.
func (s *Slave) run(ctx context.Context, cancel context.CancelCauseFunc, t task) (int64, error) {
	dir, err := os.MkdirTemp(s.dir, "job-")
	if err != nil {
		return 0, failf("the spool: %v", err)
	}
	defer os.RemoveAll(dir)
	in, err := serveInput(ctx, cancel, t.source, t.size, s.logger)
	if err != nil {
		return 0, failf("serving the source to ffmpeg: %v", err)
	}
	defer in.close()

	hwaccel := ""
	if t.output.Video != nil {
		hwaccel = s.hwaccel
	}
	output, err := s.make(ctx, t, in.url, dir, hwaccel)
	var failed ffmpegFailure
	if hwaccel != "" && errors.As(err, &failed) {
		s.logger.Warn("transcode: hardware decoding failed, decoding in software", "from", t.origin, "label", t.label, "err", failed.Error())
		output, err = s.make(ctx, t, in.url, dir, "")
	}
	in.close()
	if errors.As(err, &failed) {
		err = failed.Failure // the job's, as a slave answers one
	}
	if err != nil {
		return 0, err
	}
	out, err := os.Open(output)
	if err != nil {
		return 0, failf("the spool: %v", err)
	}
	defer out.Close()
	info, err := out.Stat()
	if err != nil {
		return 0, failf("the spool: %v", err)
	}
	return t.deliver(out, info.Size())
}

// ffmpegFailure is the job's Failure when ffmpeg exits failing, as it does when the decoding fails, which a run
// decoding otherwise may not repeat — unlike the slave's own refusals of what ffmpeg wrote.
type ffmpegFailure struct{ *transcode.Failure }

func (f ffmpegFailure) Unwrap() error { return f.Failure }

// make runs ffmpeg making t's output of the source at the URL input, decoding by the hwaccel (empty: in software), in
// a directory of its own in the job's, dir: it answers the output's path, the one file ffmpeg wrote.
func (s *Slave) make(ctx context.Context, t task, input, dir, hwaccel string) (string, error) {
	dir, err := os.MkdirTemp(dir, "run-")
	if err != nil {
		return "", failf("the spool: %v", err)
	}
	output := filepath.Join(dir, "output")
	cmd := exec.CommandContext(ctx, s.ffmpeg, s.command(t.output, input, output, hwaccel)...)
	stderr := &tail{}
	cmd.Stderr = stderr
	progress, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		return "", failf("ffmpeg: %v", err)
	}
	// A block of -progress ends with its progress line; out_time_us is N/A, or below 0, before the first frame.
	var outTime string
	for lines := bufio.NewScanner(progress); lines.Scan(); {
		key, value, _ := strings.Cut(lines.Text(), "=")
		switch key {
		case "out_time_us":
			outTime = value
		case "progress":
			if us, err := strconv.ParseInt(outTime, 10, 64); err == nil && us >= 0 {
				t.report(transcode.Advanced{Seconds: float64(us) / 1e6})
			}
		}
	}
	err = cmd.Wait()
	switch {
	case ctx.Err() != nil:
		return "", context.Cause(ctx)
	case err != nil:
		return "", ffmpegFailure{&transcode.Failure{Message: fmt.Sprintf("transcode: ffmpeg could not make the output (%v)", err), Detail: stderr.String()}}
	}

	entries, err := os.ReadDir(dir)
	switch {
	case err != nil:
		return "", failf("the spool: %v", err)
	case len(entries) == 0:
		return "", failf("format %q wrote no file", t.output.Format)
	case len(entries) > 1 || entries[0].Name() != "output" || !entries[0].Type().IsRegular():
		return "", failf("format %q writes more than one file", t.output.Format)
	}
	return output, nil
}

// tailSize is how much of ffmpeg's stderr a failure carries: the last of it.
const tailSize = 4 << 10

// tail keeps the last tailSize bytes written to it: ffmpeg's stderr, whose end says why it failed.
type tail struct {
	b       []byte
	trimmed bool
}

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if over := len(t.b) - tailSize; over > 0 {
		t.b = t.b[:copy(t.b, t.b[over:])]
		t.trimmed = true
	}
	return len(p), nil
}

// String is the whole lines kept.
func (t *tail) String() string {
	s := string(t.b)
	if t.trimmed {
		_, s, _ = strings.Cut(s, "\n")
	}
	return strings.TrimSpace(s)
}
