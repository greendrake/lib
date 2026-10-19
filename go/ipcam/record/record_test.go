package record

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/pkg/codecs/h264"
	mediah265 "github.com/bluenviron/mediacommon/pkg/codecs/h265"
	"greendrake.nz/go/camstream"
	"greendrake.nz/go/camstream/camstreamtest"
	"greendrake.nz/go/camstream/h265"
	"greendrake.nz/go/ipcam/internal/mkvtest"
	"greendrake.nz/go/ipcam/record/mkv"
)

const ms = time.Millisecond

// parameterSets are the VPS, SPS and PPS of an XM530 camera's extra stream, 800×448.
var parameterSets = func() [][]byte {
	nalus, err := camstreamtest.KeyFrame()
	if err != nil {
		panic(err)
	}
	var sets [][]byte
	for _, nalu := range nalus {
		if slices.Contains(h265.ParameterSets, h265.TypeOf(nalu)) {
			sets = append(sets, nalu)
		}
	}
	return sets
}()

// picture is an access unit of a single slice of NAL unit type kind, the parameter sets
// ahead of it where it is a random access picture's (16 to 21).
func picture(kind mediah265.NALUType) []byte {
	if kind < 16 || kind > 21 {
		return camstreamtest.AnnexB(camstreamtest.NALUnit(kind))
	}
	return camstreamtest.AnnexB(slices.Concat(parameterSets, [][]byte{camstreamtest.NALUnit(kind)})...)
}

// videoOf is a frame of a single H.265 slice of NAL unit type kind, a key frame where that
// is a random access picture's.
func videoOf(pts time.Duration, kind mediah265.NALUType) *camstream.Frame {
	return &camstream.Frame{Kind: camstream.Video, Codec: camstream.H265, Keyframe: kind >= 16 && kind <= 21, PTS: pts, Duration: 40 * ms, Data: picture(kind)}
}

// video is a frame of a single H.265 slice: IDR for a key frame, trailing otherwise.
func video(pts time.Duration, keyframe bool) *camstream.Frame {
	if keyframe {
		return videoOf(pts, 19)
	}
	return videoOf(pts, 1)
}

func audio(pts time.Duration, rate int) *camstream.Frame {
	return &camstream.Frame{Kind: camstream.Audio, Codec: camstream.PCMA, SampleRate: rate, PTS: pts, Data: make([]byte, 160)}
}

// block is one block of a recording: its track and when it is due in the file.
type block struct {
	track uint64
	at    time.Duration
}

// blocksOf are a recording's blocks, in order.
func blocksOf(f mkvtest.File) []block {
	var blocks []block
	for _, c := range f.Clusters {
		for _, b := range c.Blocks {
			blocks = append(blocks, block{track: b.Track, at: time.Duration(b.Time) * ms})
		}
	}
	return blocks
}

// files are the recordings made under dir, in the order they were made.
func files(t *testing.T, dir string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*", "*", "*", "*.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// read reads a recording back, as far as it is written.
func read(t *testing.T, name string) mkvtest.File {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	f, err := mkvtest.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// recordings reads back the recordings made under dir, in the order they were made.
func recordings(t *testing.T, dir string) []mkvtest.File {
	t.Helper()
	var all []mkvtest.File
	for _, name := range files(t, dir) {
		all = append(all, read(t, name))
	}
	return all
}

// recorded reads back the one recording made under dir.
func recorded(t *testing.T, dir string) mkvtest.File {
	t.Helper()
	all := recordings(t, dir)
	if len(all) != 1 {
		t.Fatalf("expected one recording, found %d", len(all))
	}
	return all[0]
}

// clock is a wall clock a second on at every reading, which names each file apart.
func clock() func() time.Time {
	at := time.Date(2026, 10, 16, 9, 30, 0, 0, time.Local)
	return func() time.Time {
		at = at.Add(time.Second)
		return at
	}
}

// recordingIn is the recording of a stream to dir.
func recordingIn(dir string, audio bool) *recording {
	return &recording{recorder: &recorder{dir: dir, stream: "extra", now: clock(), create: createFile}, audio: audio}
}

func writeAll(t *testing.T, rec *recording, frames ...*camstream.Frame) {
	t.Helper()
	for _, fr := range frames {
		if err := rec.write(fr); err != nil {
			t.Fatal(err)
		}
	}
}

func sameBlocks(t *testing.T, got, want []block) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("recorded %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recorded %+v, want %+v", got, want)
		}
	}
}

func TestRecordingIsTimedFromItsKeyFrame(t *testing.T) {
	dir := t.TempDir()
	rec := recordingIn(dir, true)
	writeAll(t, rec,
		audio(4980*ms, mkv.AudioRate),
		video(4960*ms, false),
		video(5000*ms, true),
		audio(4990*ms, mkv.AudioRate),
		audio(5020*ms, mkv.AudioRate),
		video(5040*ms, false),
		audio(5030*ms, mkv.AudioRate),
	)
	rec.close()
	// Nothing before the key frame the file starts with, audio due before it included;
	// audio that steps back behind the video it follows keeps its own time.
	sameBlocks(t, blocksOf(recorded(t, dir)), []block{{1, 0}, {2, 20 * ms}, {1, 40 * ms}, {2, 30 * ms}})
}

func TestRecordingWithoutAudio(t *testing.T) {
	dir := t.TempDir()
	rec := recordingIn(dir, false)
	writeAll(t, rec, video(0, true), audio(20*ms, mkv.AudioRate), video(40*ms, false))
	rec.close()
	sameBlocks(t, blocksOf(recorded(t, dir)), []block{{1, 0}, {1, 40 * ms}})
}

// A stream without audio goes on in a new file at the key frame due once a file is full,
// the file before it finished first.
func TestRecordingStartsANewFileOnceFull(t *testing.T) {
	dir := t.TempDir()
	rec := recordingIn(dir, false)
	writeAll(t, rec, video(time.Second, true), video(time.Second+fileDuration-40*ms, true))
	if rec.current.start != time.Second {
		t.Fatal("a key frame within the file's span started a new file")
	}
	writeAll(t, rec, video(time.Second+fileDuration, true))
	names := files(t, dir)
	if len(names) != 2 {
		t.Fatalf("recordings %v", names)
	}
	first := read(t, names[0])
	sameBlocks(t, blocksOf(first), []block{{1, 0}, {1, fileDuration - 40*ms}})
	if first.Size == mkvtest.Unknown {
		t.Fatal("the first file was not finished")
	}
	writeAll(t, rec, video(time.Second+fileDuration+40*ms, false))
	rec.close()
	sameBlocks(t, blocksOf(read(t, names[1])), []block{{1, 0}, {1, 40 * ms}})
}

// A file goes on taking the audio due in it, which comes a little behind the video it goes
// with, once the next file has started: until audio due in the next comes, the next key
// frame does, or the recording ends.
func TestAudioDueInTheFileBefore(t *testing.T) {
	dir := t.TempDir()
	rec := recordingIn(dir, true)
	start := time.Second
	writeAll(t, rec,
		video(start, true),
		video(start+fileDuration, true),
		audio(start+fileDuration-20*ms, mkv.AudioRate),
	)
	names := files(t, dir)
	if len(names) != 2 || read(t, names[0]).Size != mkvtest.Unknown {
		t.Fatalf("recordings %v, the first finished before its audio", names)
	}
	writeAll(t, rec, audio(start+fileDuration+20*ms, mkv.AudioRate))
	if read(t, names[0]).Size == mkvtest.Unknown {
		t.Fatal("the first file was not finished once its audio was in")
	}
	// The second waits for its audio until the next key frame, the third until the end.
	writeAll(t, rec, video(start+2*fileDuration, true))
	if read(t, files(t, dir)[1]).Size != mkvtest.Unknown {
		t.Fatal("the second file was finished before its audio")
	}
	writeAll(t, rec, video(start+2*fileDuration+2*time.Second, true))
	if read(t, files(t, dir)[1]).Size == mkvtest.Unknown {
		t.Fatal("the second file was not finished at the next key frame")
	}
	writeAll(t, rec, video(start+3*fileDuration, true))
	rec.close()
	all := recordings(t, dir)
	if len(all) != 4 {
		t.Fatalf("%d recordings", len(all))
	}
	for i, want := range [][]block{
		{{1, 0}, {2, fileDuration - 20*ms}},
		{{1, 0}, {2, 20 * ms}},
		{{1, 0}, {1, 2 * time.Second}},
		{{1, 0}},
	} {
		if all[i].Size == mkvtest.Unknown {
			t.Fatalf("recording %d not finished", i)
		}
		sameBlocks(t, blocksOf(all[i]), want)
	}
}

// A file is whole while it is written: its video described by the key frame it starts at,
// from the start, and every frame written so far. Closed, it is finished.
func TestAFileIsWholeWhileItIsWritten(t *testing.T) {
	described, err := h265.DecodingOf(parameterSets)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	rec := recordingIn(dir, true)
	writeAll(t, rec, video(time.Second, true), audio(time.Second+20*ms, mkv.AudioRate))
	path := rec.current.path
	unfinished := read(t, path)
	track := unfinished.Tracks[0]
	if track.Width != 800 || track.Height != 448 || !bytes.Equal(track.CodecPrivate, described.Config) || unfinished.Size != mkvtest.Unknown {
		t.Fatalf("an unfinished file of %+v", unfinished)
	}
	sameBlocks(t, blocksOf(unfinished), []block{{1, 0}, {2, 20 * ms}})
	writeAll(t, rec, video(time.Second+40*ms, false), video(2*time.Second, true), video(2*time.Second+40*ms, false))
	if err := rec.close(); err != nil {
		t.Fatal(err)
	}
	if finished := read(t, path); finished.Info.Duration != 1080 || finished.Size == mkvtest.Unknown || len(finished.Cues) != 2 {
		t.Fatalf("a finished file of %+v", finished)
	}
}

// A key frame of any kind of random access picture starts a file, and goes in as a key
// frame, its parameter sets ahead of it; one that carries none starts no file, which is
// reported once rather than at every such key frame.
func TestKeyFrames(t *testing.T) {
	dir := t.TempDir()
	rec := recordingIn(dir, false)
	bare := videoOf(0, 19)
	bare.Data = camstreamtest.AnnexB(camstreamtest.NALUnit(19))
	if err := rec.write(bare); err == nil || len(files(t, dir)) != 0 {
		t.Fatalf("a key frame of no parameter sets: %v, files %v", err, files(t, dir))
	}
	if err := rec.write(bare); err != nil {
		t.Fatalf("the same failure reported again: %v", err)
	}
	for i, kind := range []mediah265.NALUType{21, 1, 16, 20} {
		writeAll(t, rec, videoOf(time.Duration(i)*40*ms, kind))
	}
	rec.close()
	var keys []bool
	var leads [][]byte
	for _, c := range recorded(t, dir).Clusters {
		for _, b := range c.Blocks {
			keys = append(keys, b.Key)
			var nalus h264.AVCC
			if err := nalus.Unmarshal(b.Data); err != nil {
				t.Fatal(err)
			}
			leads = append(leads, nalus[0])
		}
	}
	if len(keys) != 4 || !keys[0] || keys[1] || !keys[2] || !keys[3] {
		t.Fatalf("key frames %v", keys)
	}
	for i, lead := range leads {
		if keys[i] != bytes.Equal(lead, parameterSets[0]) {
			t.Fatalf("block %d led by % x", i, lead)
		}
	}
}

// A video frame of no NAL units is reported, and the file goes on.
func TestAFrameOfNoNALUnits(t *testing.T) {
	dir := t.TempDir()
	rec := recordingIn(dir, false)
	writeAll(t, rec, video(0, true))
	unreadable := video(40*ms, false)
	unreadable.Data = []byte{1, 2, 3}
	if err := rec.write(unreadable); err == nil {
		t.Fatal("a frame of no NAL units written")
	}
	writeAll(t, rec, video(80*ms, false))
	rec.close()
	sameBlocks(t, blocksOf(recorded(t, dir)), []block{{1, 0}, {1, 80 * ms}})
}

// A file the writer fails is closed, the error reported, and the next key frame starts
// another. The failed file holds what went in before, its places held.
func TestAFailedFileGivesWayToTheNext(t *testing.T) {
	dir := t.TempDir()
	rec := recordingIn(dir, false)
	disk := &mkvtest.Disk{}
	rec.create = func(string) (mkv.WriteSeekCloser, error) { return disk, nil }
	writeAll(t, rec, video(0, true))
	disk.Full = true
	if err := rec.write(video(40*ms, false)); !errors.Is(err, mkvtest.ErrFull) || rec.current != nil || !disk.Closed {
		t.Fatalf("got %v, file open %v, closed %v", err, rec.current != nil, disk.Closed)
	}
	failed, err := mkvtest.Read(disk.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if failed.Size != mkvtest.Unknown || failed.Info.Duration != 0 || len(failed.Seeks) != 2 {
		t.Fatalf("a failed file of %+v", failed)
	}
	sameBlocks(t, blocksOf(failed), []block{{1, 0}})
	rec.create = createFile
	writeAll(t, rec, video(time.Second, true))
	if rec.current == nil {
		t.Fatal("the next key frame started no file")
	}
	rec.close()
}

func TestRecordingRefusesAudioItsTrackCannotHold(t *testing.T) {
	dir := t.TempDir()
	rec := recordingIn(dir, true)
	writeAll(t, rec, video(0, true))
	if err := rec.write(audio(20*ms, 16000)); err == nil || !strings.Contains(err.Error(), "16000 Hz") {
		t.Fatalf("got %v", err)
	}
	// Reported once a file, not once a frame.
	writeAll(t, rec, audio(40*ms, 16000), audio(60*ms, mkv.AudioRate), video(80*ms, false))
	rec.close()
	sameBlocks(t, blocksOf(recorded(t, dir)), []block{{1, 0}, {1, 80 * ms}})
}

// A recorder subscribes again once its subscription ends with its monitor, and starts a
// file per subscription, with audio as its monitor carries it.
func TestRecorderStartsAFilePerSubscription(t *testing.T) {
	cam := camstreamtest.NewCamera(t)
	ctx, cancel := context.WithCancel(context.Background())
	source := camstream.NewSource(ctx, cam.Open)
	dir := t.TempDir()
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&recorder{dir: dir, stream: "extra", now: clock(), create: createFile, logger: slog.Default()}).run(ctx, source)
	}()
	// The first monitor carries audio, the second none.
	first, second := camstreamtest.NewMonitor(), camstreamtest.NewMonitor()
	first.Audio = true
	cam.Opens(first)
	first.Frames <- video(time.Second, true)
	first.Frames <- audio(time.Second+20*ms, mkv.AudioRate)
	first.End()
	cam.Opens(second)
	second.Frames <- video(5*time.Second, true)
	second.Frames <- video(5*time.Second+40*ms, false)
	// Taken only once the frame before it has reached the recorder, and recorded not at
	// all: the stream has no audio.
	second.Frames <- audio(5*time.Second+60*ms, mkv.AudioRate)
	cancel()
	<-done
	<-source.Done()
	all := recordings(t, dir)
	if len(all) != 2 || len(all[0].Tracks) != 2 || len(all[1].Tracks) != 1 {
		t.Fatalf("recordings %+v", all)
	}
	sameBlocks(t, blocksOf(all[0]), []block{{1, 0}, {2, 20 * ms}})
	sameBlocks(t, blocksOf(all[1]), []block{{1, 0}, {1, 40 * ms}})
}

// A camera's stream, as the camera sent it, recorded as it came: 4.2 s of an 800×448
// extra stream at 15 frames a second, with 8 kHz A-law audio.
func TestRecordingOfACamerasStream(t *testing.T) {
	frames, err := camstreamtest.Recording()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	rec := recordingIn(dir, true)
	var want []block
	for _, fr := range frames {
		track := uint64(1)
		if fr.Kind == camstream.Audio {
			track = 2
		}
		writeAll(t, rec, fr)
		want = append(want, block{track: track, at: fr.PTS.Truncate(ms)})
	}
	rec.close()
	got := recorded(t, dir)
	sameBlocks(t, blocksOf(got), want)
	if video := got.Tracks[0]; video.Width != 800 || video.Height != 448 {
		t.Fatalf("video track %dx%d", video.Width, video.Height)
	}
}
