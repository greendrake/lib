// Package record records camera streams to MKV files.
package record

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/bluenviron/mediacommon/pkg/codecs/h264"
	"greendrake.nz/go/camstream"
	"greendrake.nz/go/camstream/h265"
	"greendrake.nz/go/ipcam/internal/failures"
	"greendrake.nz/go/ipcam/record/mkv"
)

// fileDuration is how much of a stream a file holds, give or take the key frame the next
// one starts at.
const fileDuration = 10 * time.Minute

// Run records source's stream to dir until ctx ends, in files of fileDuration, each
// starting at a key frame, at <dir>/YYYY/MM/DD/HH-mm-ss.<stream>.mkv. It subscribes again
// at once whenever its subscription ends, and each subscription starts a new file, with
// audio if its monitor carries it. What fails is logged to logger.
func Run(ctx context.Context, source *camstream.Source, dir, stream string, logger *slog.Logger) {
	(&recorder{dir: dir, stream: stream, now: time.Now, create: createFile, logger: logger}).run(ctx, source)
}

// createFile creates the file name names, as files are written.
func createFile(name string) (mkv.WriteSeekCloser, error) {
	return os.Create(name)
}

// recorder writes the files of one stream.
type recorder struct {
	dir, stream string
	// When a file starts, by the wall clock, which names and dates it.
	now func() time.Time
	// What a file is written through, created where it is named.
	create func(name string) (mkv.WriteSeekCloser, error)
	logger *slog.Logger
}

func (r *recorder) run(ctx context.Context, source *camstream.Source) {
	for ctx.Err() == nil {
		r.record(source.Subscribe(ctx))
	}
}

// record writes a subscription's frames to the files it starts, until it ends.
func (r *recorder) record(sub *camstream.Subscription) {
	first, live := <-sub.Frames()
	if !live {
		return
	}
	// Known once the subscription has delivered a frame.
	rec := &recording{recorder: r, audio: sub.HasAudio()}
	report := func(err error) {
		if err != nil {
			r.logger.Warn("ipcam: a recording fails", "dir", r.dir, "stream", r.stream, "err", err)
		}
	}
	defer func() { report(rec.close()) }()
	for fr := first; live; fr, live = <-sub.Frames() {
		report(rec.write(fr))
	}
}

// recording is a subscription's recording, file after file.
type recording struct {
	*recorder
	// Whether the stream carries audio.
	audio bool
	// The file being written: none before the first key frame, nor once a file has
	// failed or could not be started, until a key frame starts another.
	current *file
	// The file before it, kept open for the audio due in it, which comes a little behind
	// the video it goes with: until audio due at end or later comes, or the next key frame
	// does. None without audio.
	previous *file
	// Where previous ends: at the key frame that started the file after it, or was to.
	end time.Duration
	// The failures to open a file, retried at every key frame: each reported while it is
	// news, rather than once a key frame.
	opening failures.Last
}

// file is a file open for writing.
type file struct {
	writer *mkv.Writer
	path   string
	// Where the file starts on the stream's clock.
	start time.Duration
	// Set once the file has met audio its track cannot hold, which is reported once a
	// file rather than once a frame.
	audioRefused bool
}

// closeFile finishes the file target holds, if it holds one, and lets it go.
func closeFile(target **file) error {
	f := *target
	if f == nil {
		return nil
	}
	*target = nil
	if err := f.writer.Close(); err != nil {
		return fmt.Errorf("%s: %w", f.path, err)
	}
	return nil
}

// close finishes the files before anything goes on: a process that ends once it has
// returned leaves them whole.
func (r *recording) close() error {
	return errors.Join(closeFile(&r.previous), closeFile(&r.current))
}

// write writes a frame: video to the current file, starting the next at the key frame
// that is due once the current one is full, and audio to the file it is due in.
func (r *recording) write(fr *camstream.Frame) error {
	if fr.Kind != camstream.Video {
		if r.previous != nil && fr.PTS < r.end {
			return r.put(&r.previous, fr, nil)
		}
		// The audio has caught up with the current file.
		return errors.Join(closeFile(&r.previous), r.put(&r.current, fr, nil))
	}
	var nalus h264.AnnexB
	if err := nalus.Unmarshal(fr.Data); err != nil {
		return fmt.Errorf("a video frame of no NAL units: %w", err)
	}
	var closed error
	if fr.Keyframe {
		// A file waiting for its audio has had it by the next key frame, whether the audio
		// came or not.
		closed = closeFile(&r.previous)
		if r.current == nil || fr.PTS-r.current.start >= fileDuration {
			r.previous, r.current, r.end = r.current, nil, fr.PTS
			if !r.audio {
				closed = errors.Join(closed, closeFile(&r.previous))
			}
			if err := r.open(fr.PTS, nalus); err != nil {
				if !r.opening.News(err) {
					return closed
				}
				return errors.Join(closed, err)
			}
			r.opening.Succeeded()
		}
	}
	return errors.Join(closed, r.put(&r.current, fr, nalus))
}

// put writes a frame to the file target holds, a video frame as its NAL units. A file the
// writer has failed is no good: it is closed, and let go.
func (r *recording) put(target **file, fr *camstream.Frame, nalus [][]byte) error {
	f := *target
	// Nothing goes in before the key frame a file starts with: neither the frames before
	// the first one, nor audio that was due before it.
	if f == nil || fr.PTS < f.start {
		return nil
	}
	at := fr.PTS - f.start
	var err error
	switch {
	case fr.Kind == camstream.Video:
		err = f.writer.WriteVideo(at, fr.Duration, fr.Keyframe, nalus)
	case !r.audio || f.audioRefused:
	case fr.Codec != camstream.PCMA || fr.SampleRate != mkv.AudioRate:
		f.audioRefused = true
		return fmt.Errorf("recording without audio until the next file: its track holds A-law at %d Hz, the stream has codec %d at %d Hz", mkv.AudioRate, fr.Codec, fr.SampleRate)
	default:
		err = f.writer.WriteAudio(at, fr.Data)
	}
	if err != nil {
		// Its close fails for what the write did, err.
		closeFile(target)
		return fmt.Errorf("%s: %w; the file is closed as it is", f.path, err)
	}
	return nil
}

// open starts the current file at the key frame at at, whose NAL units' parameter sets
// describe its video.
func (r *recording) open(at time.Duration, nalus [][]byte) error {
	video, err := h265.DecodingOf(nalus)
	if err != nil {
		return err
	}
	now := r.now()
	path := filepath.Join(r.dir, now.Format("2006/01/02/15-04-05.")+r.stream+".mkv")
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	out, err := r.create(path)
	if err != nil {
		return err
	}
	writer, err := mkv.Create(out, video, r.audio, now)
	if err != nil {
		out.Close()
		return err
	}
	r.current = &file{writer: writer, path: path, start: at}
	return nil
}
