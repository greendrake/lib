// Package transcodetest is the module's test support: slaves of the ffmpeg on PATH, media made from ffmpeg's test
// sources, and what ffprobe finds in an output. The tests need an ffmpeg and an ffprobe on PATH.
package transcodetest

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"greendrake.nz/go/transcode"
	"greendrake.nz/go/transcode/slave"
)

// Tool is the path of a tool on PATH: ffmpeg, ffprobe.
func Tool(t testing.TB, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("the tests need %s: %v", name, err)
	}
	return path
}

// Slave is a slave of the ffmpeg on PATH, spooling in a directory of the test's.
type Slave struct {
	*slave.Slave
	Spool    string
	outcomes Outcomes
}

// NewSlave is a slave of cfg, its FFmpeg the one on PATH where cfg names none, its Spool and Logger the test's; it is
// closed with the test.
func NewSlave(t testing.TB, cfg slave.Config) *Slave {
	t.Helper()
	s := &Slave{Spool: t.TempDir(), outcomes: make(Outcomes, 256)}
	if cfg.FFmpeg == "" {
		cfg.FFmpeg = Tool(t, "ffmpeg")
	}
	cfg.Spool, cfg.Logger = s.Spool, slog.New(s.outcomes)
	var err error
	if s.Slave, err = slave.New(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// Serve serves s's handler for the test, answering its WebSocket URL.
func (s *Slave) Serve(t testing.TB) string {
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// Outcome waits for the outcome of the next job s logs as ended.
func (s *Slave) Outcome() string { return <-s.outcomes }

// Jobs are the directories in s's own: those of its jobs running.
func (s *Slave) Jobs(t testing.TB) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(s.Spool, "transcode-*", "*"))
	if err != nil {
		t.Fatal(err)
	}
	return dirs
}

// Outcomes is a slog.Handler passing on the outcome of each job a slave logs as ended.
type Outcomes chan string

func (o Outcomes) Enabled(context.Context, slog.Level) bool { return true }

func (o Outcomes) Handle(_ context.Context, r slog.Record) error {
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "outcome" {
			o <- a.Value.String()
		}
		return true
	})
	return nil
}

func (o Outcomes) WithAttrs([]slog.Attr) slog.Handler { return o }
func (o Outcomes) WithGroup(string) slog.Handler      { return o }

// Pattern is the input arguments of ffmpeg's moving test pattern, w×h, seconds long.
func Pattern(w, h int, seconds float64) []string {
	// testsrc2 rounds an odd side down to even; the scale makes it w×h whatever.
	return []string{"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=%[1]dx%[2]d:rate=25:duration=%[3]g,scale=%[1]d:%[2]d", w, h, seconds)}
}

// HDR is the output arguments making HDR video: 10-bit HEVC in BT.2020, of transfer ("arib-std-b67" for HLG,
// "smpte2084" for PQ). The frames carry the tags, which libx265 then writes.
func HDR(transfer string) []string {
	return []string{"-vf", "setparams=color_primaries=bt2020:color_trc=" + transfer + ":colorspace=bt2020nc:range=tv",
		"-c:v", "libx265", "-pix_fmt", "yuv420p10le", "-x265-params", "log-level=error"}
}

// Tone is the input arguments of a tone, seconds long.
func Tone(seconds float64) []string {
	return []string{"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:duration=%g", seconds)}
}

// AVC is the output every browser plays, of a short side of 120.
var AVC = transcode.Output{Format: "mp4", Video: &transcode.Video{Codec: "h264", MaxShortSide: 120, PixelFormat: "yuv420p"}, Audio: &transcode.Audio{Codec: "aac"}}

// Clip is a two-second clip of w×h with sound, in MP4.
func Clip(t testing.TB, w, h int) []byte {
	t.Helper()
	return Make(t, "clip.mp4", append(append(Pattern(w, h, 2), Tone(2)...), "-c:v", "libx264", "-c:a", "aac")...)
}

// Silent is a one-second clip of w×h without sound, in MP4.
func Silent(t testing.TB, w, h int) []byte {
	t.Helper()
	return Make(t, "silent.mp4", append(Pattern(w, h, 1), "-c:v", "libx264")...)
}

// Job is a job making AVC of src.
func Job(src []byte) transcode.Job {
	return transcode.Job{Label: "clip", Source: bytes.NewReader(src), Size: int64(len(src)), Output: AVC}
}

// ErrFailing is what a Failing source's reads fail with.
var ErrFailing = errors.New("the disk is on fire")

// Failing is a source whose reads reaching past At fail with ErrFailing: from the start where At is 0.
type Failing struct {
	io.ReaderAt
	At int64
}

func (f Failing) ReadAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > f.At {
		return 0, ErrFailing
	}
	return f.ReaderAt.ReadAt(p, off)
}

// Gated is j with its source behind a closed gate, which it answers.
func Gated(j transcode.Job) (transcode.Job, *Gate) {
	gate := NewGate(j.Source)
	j.Source = gate
	return j, gate
}

// MakeFile is the path of the file ffmpeg writes from args, of the test's, named name for the muxer to go by.
func MakeFile(t testing.TB, name string, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	ffmpeg := exec.Command(Tool(t, "ffmpeg"), append(append([]string{"-hide_banner", "-nostdin", "-loglevel", "error"}, args...), path)...)
	if out, err := ffmpeg.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return path
}

// Make is the bytes of the file MakeFile makes.
func Make(t testing.TB, name string, args ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(MakeFile(t, name, args...))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Run has tr do the job of making o of source, answering the output.
func Run(t testing.TB, tr transcode.Transcoder, source []byte, o transcode.Output) ([]byte, error) {
	t.Helper()
	var out bytes.Buffer
	n, err := tr.Transcode(context.Background(), transcode.Job{Label: t.Name(), Source: bytes.NewReader(source), Size: int64(len(source)), Output: o}, &out)
	if n != int64(out.Len()) {
		t.Errorf("an output of %d bytes, answered as %d", out.Len(), n)
	}
	return out.Bytes(), err
}

// Background is a job being done in the background.
type Background struct {
	progress chan transcode.Progress
	Done     chan error // the job's end
}

// Go has tr do job in the background, under ctx, its output dropped.
func Go(ctx context.Context, tr transcode.Transcoder, job transcode.Job) *Background {
	b := &Background{progress: make(chan transcode.Progress, 1024), Done: make(chan error, 1)}
	job.Progress = func(p transcode.Progress) { b.progress <- p }
	go func() {
		_, err := tr.Transcode(ctx, job, io.Discard)
		b.Done <- err
	}()
	return b
}

// Await takes the job's progress until want, failing the test if the job ends first.
func (b *Background) Await(t testing.TB, want transcode.Progress) {
	t.Helper()
	for {
		select {
		case p := <-b.progress:
			if p == want {
				return
			}
		case err := <-b.Done:
			t.Fatalf("the job ended (%v) before %+v", err, want)
		}
	}
}

// Rest is the progress not yet taken, once the job has ended; it fails the test if the job failed.
func (b *Background) Rest(t testing.TB) []transcode.Progress {
	t.Helper()
	if err := <-b.Done; err != nil {
		t.Fatal(err)
	}
	var rest []transcode.Progress
	for len(b.progress) > 0 {
		rest = append(rest, <-b.progress)
	}
	return rest
}

// Gate is a source whose reads wait until it opens.
type Gate struct {
	io.ReaderAt
	reading, open        chan struct{}
	firstRead, firstOpen sync.Once
}

// NewGate is a closed gate in front of src.
func NewGate(src io.ReaderAt) *Gate {
	return &Gate{ReaderAt: src, reading: make(chan struct{}), open: make(chan struct{})}
}

func (g *Gate) ReadAt(p []byte, off int64) (int, error) {
	g.firstRead.Do(func() { close(g.reading) })
	<-g.open
	return g.ReaderAt.ReadAt(p, off)
}

// Reading is closed once the first read waits at the gate.
func (g *Gate) Reading() <-chan struct{} { return g.reading }

// Open lets the reads through.
func (g *Gate) Open() { g.firstOpen.Do(func() { close(g.open) }) }

// Media is what ffprobe finds in a file.
type Media struct {
	Format struct {
		Name     string `json:"format_name"`
		Duration string `json:"duration"`
	} `json:"format"`
	Streams  []Stream          `json:"streams"`
	Chapters []json.RawMessage `json:"chapters"`
}

// Stream is a stream of Media.
type Stream struct {
	Type        string `json:"codec_type"`
	Codec       string `json:"codec_name"`
	Tag         string `json:"codec_tag_string"`
	Profile     string `json:"profile"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	PixelFormat string `json:"pix_fmt"`
	Transfer    string `json:"color_transfer"`
	Primaries   string `json:"color_primaries"`
	Matrix      string `json:"color_space"`
	BitRate     string `json:"bit_rate"`
	SideData    []struct {
		Type string `json:"side_data_type"`
	} `json:"side_data_list"`
}

// Probe is what ffprobe finds in data.
func Probe(t testing.TB, data []byte) Media {
	t.Helper()
	out, err := exec.Command(Tool(t, "ffprobe"), "-v", "error", "-print_format", "json", "-show_format", "-show_streams", "-show_chapters", Write(t, "probed", data)).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	var m Media
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Write is the path of a file of the test's, named name, holding data.
func Write(t testing.TB, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Kinds are the kinds of m's streams, in order: "video,audio", say.
func (m Media) Kinds() string {
	kinds := make([]string, len(m.Streams))
	for i, s := range m.Streams {
		kinds[i] = s.Type
	}
	return strings.Join(kinds, ",")
}

// Seconds is m's duration.
func (m Media) Seconds(t testing.TB) float64 {
	t.Helper()
	seconds, err := strconv.ParseFloat(m.Format.Duration, 64)
	if err != nil {
		t.Fatal(err)
	}
	return seconds
}

// Boxes are the types of an MP4's top-level boxes, in order.
func Boxes(t testing.TB, mp4 []byte) []string {
	t.Helper()
	var boxes []string
	for len(mp4) > 0 {
		if len(mp4) < 8 {
			t.Fatalf("%d bytes after the boxes %v", len(mp4), boxes)
		}
		size := uint64(binary.BigEndian.Uint32(mp4))
		header := uint64(8)
		if size == 1 {
			size, header = binary.BigEndian.Uint64(mp4[8:]), 16
		}
		if size < header || size > uint64(len(mp4)) {
			t.Fatalf("a %q box of %d bytes, %d left", mp4[4:8], size, len(mp4))
		}
		boxes = append(boxes, string(mp4[4:8]))
		mp4 = mp4[size:]
	}
	return boxes
}
