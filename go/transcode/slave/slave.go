// Package slave does transcode jobs with ffmpeg on this machine, each codec made by the encoder configured for it
// here — a hardware one, say — or else by ffmpeg's default encoder for it: in-process (Transcode), and for masters
// over the transcode protocol (ServeHTTP), the two sharing one queue.
package slave

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"sync"
	"time"

	"greendrake.nz/go/transcode"
)

// Config configures a Slave. The YAML keys are the field names, as the standalone command reads them.
type Config struct {
	FFmpeg   string             `yaml:"FFmpeg"`   // the ffmpeg binary
	Spool    string             `yaml:"Spool"`    // where the slave makes its own directory, holding outputs while their jobs run
	Jobs     int                `yaml:"Jobs"`     // jobs run at once (≥ 1); others wait their turn, in arrival order
	HWAccel  string             `yaml:"HWAccel"`  // ffmpeg's -hwaccel for decoding video, e.g. "videotoolbox", what it fails decoded in software; empty for none
	Encoders map[string]Encoder `yaml:"Encoders"` // codec → the encoder making it here, where not ffmpeg's default for the codec
	Logger   *slog.Logger       `yaml:"-"`        // where jobs are logged, a line each as they end; slog.Default() when nil
}

// Encoder is the encoder making a codec on the slave's machine. One meant for hardware is configured to fail a job it
// cannot encode in hardware rather than encode it in software (VideoToolbox's default).
//
// ffmpeg leaves an encoder that sets no defaults of its own at its generic ones, and VideoToolbox's encoders are such: a
// key frame every 12 frames and no B-frames, which at the same bitrate shows clearly worse than libx264's or libx265's
// output. Their Args give them libx264's GOP: -g 250 -bf 3 (and -profile:v high for H.264).
type Encoder struct {
	Name string   `yaml:"Name" json:"name"` // ffmpeg's encoder, e.g. "h264_videotoolbox", "hevc_videotoolbox"
	Args []string `yaml:"Args" json:"args"` // its own options besides the bitrate, e.g. ["-profile:v", "high"]
}

// Health is how a slave is, for the program embedding it to serve as it likes.
type Health struct {
	FFmpeg   string             `json:"ffmpeg"`   // ffmpeg's version line
	Encoders map[string]Encoder `json:"encoders"` // the configured encoders, by codec
	Running  int                `json:"running"`  // jobs running
	Waiting  int                `json:"waiting"`  // jobs waiting for a slot
}

// Slave is a transcode.Transcoder doing its jobs with ffmpeg here, and the http.Handler doing them for masters.
type Slave struct {
	ffmpeg   string
	hwaccel  string
	encoders map[string]Encoder
	logger   *slog.Logger
	version  string          // ffmpeg's version line
	codecs   map[string]byte // the codecs ffmpeg encodes, by name: 'V' a video one, 'A' an audio one, …
	devices  map[string]bool // ffmpeg's output devices, which write no file
	dir      string          // the slave's own, in the spool
	queue    queue
	ctx      context.Context // ends with Close, and its jobs with it
	cancel   context.CancelCauseFunc
	mu       sync.Mutex
	closed   bool
	jobs     sync.WaitGroup
}

// errClosed ends the jobs of a slave that is closing.
var errClosed = fmt.Errorf("transcode: the slave is closing: %w", context.Canceled)

// New makes a slave of cfg: it checks that cfg.FFmpeg runs and has the configured encoders and hardware
// acceleration, each a key of cfg.Encoders naming a codec it encodes, and makes a directory of its own in cfg.Spool
// (transcode-*), so that nothing else there is touched. A slave that dies leaves its directory behind, for its
// operator.
func New(cfg Config) (*Slave, error) {
	if cfg.Jobs < 1 {
		return nil, fmt.Errorf("slave: Jobs is %d, not ≥ 1", cfg.Jobs)
	}
	if cfg.FFmpeg == "" {
		return nil, errors.New("slave: no FFmpeg")
	}
	if cfg.Spool == "" {
		return nil, errors.New("slave: no Spool")
	}
	s := &Slave{ffmpeg: cfg.FFmpeg, hwaccel: cfg.HWAccel, encoders: map[string]Encoder{}, logger: cfg.Logger, queue: queue{free: cfg.Jobs}}
	maps.Copy(s.encoders, cfg.Encoders) // a map however configured: Health answers one, none configured or many
	if s.logger == nil {
		s.logger = slog.Default()
	}
	if err := s.probe(); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(cfg.Spool, "transcode-")
	if err != nil {
		return nil, fmt.Errorf("slave: %w", err)
	}
	s.dir = dir
	s.ctx, s.cancel = context.WithCancelCause(context.Background())
	return s, nil
}

// Transcode does job here, in the slave's queue with its masters' jobs.
func (s *Slave) Transcode(ctx context.Context, job transcode.Job, dst io.Writer) (int64, error) {
	ctx, cancel := s.jobContext(ctx)
	defer cancel(nil)
	return s.do(ctx, cancel, task{
		origin: "local",
		label:  job.Label,
		size:   job.Size,
		output: job.Output,
		source: &local{src: job.Source},
		report: job.Report,
		deliver: func(out *os.File, _ int64) (int64, error) {
			return io.Copy(dst, out)
		},
	})
}

// Health is how s is now.
func (s *Slave) Health() Health {
	running, waiting := s.queue.counts()
	return Health{FFmpeg: s.version, Encoders: maps.Clone(s.encoders), Running: running, Waiting: waiting}
}

// Close ends s's jobs, running and waiting, refuses any more, then deletes its directory.
func (s *Slave) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel(errClosed)
	s.jobs.Wait()
	return os.RemoveAll(s.dir)
}

// task is a job as the slave does it, whoever asked for it.
type task struct {
	origin  string // who asked: the master's address, or "local"
	label   string
	size    int64
	output  transcode.Output
	source  source
	report  func(transcode.Status)
	deliver func(out *os.File, size int64) (int64, error) // hands the finished output on
}

// jobContext is a job's context: it ends with parent, or with the slave's Close.
func (s *Slave) jobContext(parent context.Context) (context.Context, context.CancelCauseFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	stop := context.AfterFunc(s.ctx, func() { cancel(context.Cause(s.ctx)) })
	return ctx, func(cause error) {
		stop()
		cancel(cause)
	}
}

// do does t under ctx, from s.jobContext, which cancel ends: its turn in the queue, then the job, then a log line.
func (s *Slave) do(ctx context.Context, cancel context.CancelCauseFunc, t task) (n int64, err error) {
	began := time.Now()
	defer func() {
		outcome, level := "ok", slog.LevelInfo
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			outcome = "cancelled"
		case err != nil:
			outcome, level = "failed: "+err.Error(), slog.LevelWarn
		}
		s.logger.Log(context.Background(), level, "transcode: job", "from", t.origin, "label", t.label, "size", t.size,
			"read", t.source.taken(), "elapsed", time.Since(began).Round(time.Millisecond), "output", n, "outcome", outcome)
	}()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, errClosed
	}
	s.jobs.Add(1)
	s.mu.Unlock()
	defer s.jobs.Done()

	if err := s.check(t.output); err != nil {
		return 0, err
	}
	release, err := s.queue.wait(ctx, t.report)
	if err != nil {
		return 0, err
	}
	defer release()
	t.report(transcode.Running{})
	return s.run(ctx, cancel, t)
}

// check answers a *transcode.Failure for an output s does not make: an invalid one, one in a device's format, or one
// in a codec its ffmpeg does not encode — an encoder's name among them, which is no codec's.
func (s *Slave) check(o transcode.Output) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if s.devices[o.Format] {
		return failf("format %q is a device, which writes no file", o.Format)
	}
	if o.Video != nil && s.codecs[o.Video.Codec] != 'V' {
		return failf("%q is no video codec this slave's ffmpeg encodes", o.Video.Codec)
	}
	if o.Audio != nil && s.codecs[o.Audio.Codec] != 'A' {
		return failf("%q is no audio codec this slave's ffmpeg encodes", o.Audio.Codec)
	}
	return nil
}

// failf is a *transcode.Failure saying what format and args make, with no detail.
func failf(format string, args ...any) error {
	return &transcode.Failure{Message: "transcode: " + fmt.Sprintf(format, args...)}
}
