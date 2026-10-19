package mkv

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/pkg/codecs/h264"
	"greendrake.nz/go/camstream/camstreamtest"
	"greendrake.nz/go/camstream/h265"
	"greendrake.nz/go/ipcam/internal/ebml"
	"greendrake.nz/go/ipcam/internal/mkvtest"
)

const (
	ms = time.Millisecond
	// How long a picture is on screen.
	shown = 40 * ms
)

// slice is a picture's NAL unit, a trailing picture's.
var slice = [][]byte{camstreamtest.NALUnit(1)}

// samples are 20 ms of A-law.
var samples = make([]byte, 160)

// keyFrame is a camera's key frame, and the description of its 800×448 stream.
func keyFrame(t *testing.T) ([][]byte, h265.Decoding) {
	t.Helper()
	nalus, err := camstreamtest.KeyFrame()
	if err != nil {
		t.Fatal(err)
	}
	video, err := h265.DecodingOf(nalus)
	if err != nil {
		t.Fatal(err)
	}
	return nalus, video
}

// create creates a file on a disk in memory.
func create(t *testing.T, audio bool) (*Writer, *mkvtest.Disk) {
	t.Helper()
	_, video := keyFrame(t)
	disk := &mkvtest.Disk{}
	w, err := Create(disk, video, audio, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return w, disk
}

func read(t *testing.T, disk *mkvtest.Disk) mkvtest.File {
	t.Helper()
	f, err := mkvtest.Read(disk.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// avcc is an access unit's NAL units as a block holds them.
func avcc(t *testing.T, nalus [][]byte) []byte {
	t.Helper()
	b, err := h264.AVCC(nalus).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func check(t *testing.T, errs ...error) {
	t.Helper()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
}

// A fresh file's head is whole: its tracks as they are, its Info but the duration, and a
// SeekHead of the Info and the Tracks.
func TestAFreshFilesHead(t *testing.T) {
	_, video := keyFrame(t)
	date := time.Date(2026, 10, 17, 9, 30, 0, 123456789, time.UTC)
	disk := &mkvtest.Disk{}
	if _, err := Create(disk, video, true, date); err != nil {
		t.Fatal(err)
	}
	f := read(t, disk)
	if f.DocType != "matroska" || f.Size != mkvtest.Unknown || f.Clusters != nil || f.Cues != nil {
		t.Fatalf("a fresh file of %+v", f)
	}
	info := f.Info
	if info.TimestampScale != 1e6 || info.MuxingApp != app || info.WritingApp != app || !info.Date.Equal(date) || info.Duration != 0 {
		t.Fatalf("Info %+v", info)
	}
	if _, has := f.Seeks[ebml.Info]; !has || len(f.Seeks) != 2 {
		t.Fatalf("seeks %v", f.Seeks)
	}
	if _, has := f.Seeks[ebml.Tracks]; !has {
		t.Fatalf("seeks %v", f.Seeks)
	}
	want := []mkvtest.Track{
		{Number: 1, UID: 1, Type: 1, CodecID: "V_MPEGH/ISO/HEVC", CodecPrivate: video.Config, Width: 800, Height: 448},
		{
			Number: 2, UID: 2, Type: 2, CodecID: "A_MS/ACM",
			// A-law, mono, 8,000 samples and bytes a second, a byte a block, 8 bits a sample.
			CodecPrivate:      []byte{6, 0, 1, 0, 0x40, 0x1f, 0, 0, 0x40, 0x1f, 0, 0, 1, 0, 8, 0, 0, 0},
			SamplingFrequency: AudioRate,
			Channels:          1,
		},
	}
	if !reflect.DeepEqual(f.Tracks, want) {
		t.Fatalf("tracks %+v", f.Tracks)
	}
}

func TestAFileWithoutAudio(t *testing.T) {
	w, disk := create(t, false)
	if err := w.WriteAudio(0, samples); err == nil {
		t.Fatal("audio written to a file without its track")
	}
	if tracks := read(t, disk).Tracks; len(tracks) != 1 || tracks[0].Type != videoType {
		t.Fatalf("tracks %+v", tracks)
	}
}

// A file read between writes holds every block written to it, in its open Cluster of
// unknown size.
func TestAFileHoldsEveryBlockAsItIsWritten(t *testing.T) {
	key, _ := keyFrame(t)
	w, disk := create(t, true)
	check(t, w.WriteVideo(1000*ms, shown, true, key), w.WriteAudio(1020*ms, samples), w.WriteVideo(1040*ms, shown, false, slice))
	want := []mkvtest.Block{
		{Track: videoTrack, Time: 1000, Relative: 0, Key: true, Data: avcc(t, key)},
		{Track: audioTrack, Time: 1020, Relative: 20, Key: true, Data: samples},
		{Track: videoTrack, Time: 1040, Relative: 40, Key: false, Data: avcc(t, slice)},
	}
	clusters := read(t, disk).Clusters
	if len(clusters) != 1 || clusters[0].Size != mkvtest.Unknown || clusters[0].Time != 1000 || !reflect.DeepEqual(clusters[0].Blocks, want) {
		t.Fatalf("clusters %+v", clusters)
	}
}

// Clusters open at key frames, with a CuePoint each, and wherever a block's time relative
// to the open Cluster's would leave 16 bits, with none. Each finished Cluster has its size;
// audio behind the Cluster it goes in keeps its own time.
func TestClusters(t *testing.T) {
	key, _ := keyFrame(t)
	w, disk := create(t, true)
	check(t,
		w.WriteVideo(40*time.Second, shown, true, key),
		w.WriteAudio((40000-32768)*ms, samples),
		w.WriteAudio((40000-32769)*ms, samples),
		w.WriteVideo(50*time.Second, shown, true, key),
		w.WriteVideo((50000+32767)*ms, shown, false, slice),
		w.WriteVideo((50000+32768)*ms, shown, false, slice),
	)
	type block struct {
		time     int64
		relative int16
	}
	want := [][]block{
		{{40000, 0}, {40000 - 32768, -32768}},
		{{40000 - 32769, 0}},
		{{50000, 0}, {50000 + 32767, 32767}},
		{{50000 + 32768, 0}},
	}
	clusters := read(t, disk).Clusters
	if len(clusters) != len(want) {
		t.Fatalf("%d clusters", len(clusters))
	}
	for i, c := range clusters {
		var got []block
		for _, b := range c.Blocks {
			got = append(got, block{b.Time, b.Relative})
		}
		if !reflect.DeepEqual(got, want[i]) || (c.Size == mkvtest.Unknown) != (i == len(want)-1) {
			t.Fatalf("cluster %d of %d bytes, blocks %v", i, c.Size, got)
		}
	}
	check(t, w.Close())
	f := read(t, disk)
	cues := []mkvtest.CuePoint{{Time: 40000, Track: videoTrack, Position: clusters[0].Position}, {Time: 50000, Track: videoTrack, Position: clusters[2].Position}}
	if !reflect.DeepEqual(f.Cues, cues) || f.Clusters[3].Size == mkvtest.Unknown {
		t.Fatalf("cues %+v, the last cluster of %d bytes", f.Cues, f.Clusters[3].Size)
	}
}

// A finished file has its duration, up to where the block that ends last ends, the
// SeekHead's entry for its Cues and the Segment's size, which the reader checks to end
// where the file does.
func TestAFinishedFile(t *testing.T) {
	key, _ := keyFrame(t)
	w, disk := create(t, true)
	check(t,
		w.WriteVideo(1000*ms, shown, true, key),
		w.WriteAudio(1020*ms, samples),
		w.WriteVideo(1040*ms, shown, false, slice),
		w.WriteVideo(2000*ms, shown, true, key),
		w.WriteAudio(2060*ms, samples),
		w.WriteVideo(2040*ms, shown, false, slice),
		w.Close(),
	)
	f := read(t, disk)
	if _, has := f.Seeks[ebml.Cues]; !has || len(f.Seeks) != 3 || f.Info.Duration != 2080 || f.Size == mkvtest.Unknown || len(f.Cues) != 2 || !disk.Closed {
		t.Fatalf("a finished file of %+v, closed %v", f, disk.Closed)
	}
}

// The audio's samples say how long its blocks last.
func TestAFileEndingInAudio(t *testing.T) {
	key, _ := keyFrame(t)
	w, disk := create(t, true)
	check(t, w.WriteVideo(0, shown, true, key), w.WriteAudio(30*ms, samples), w.Close())
	if f := read(t, disk); f.Info.Duration != 50 {
		t.Fatalf("a file of %v ms", f.Info.Duration)
	}
}

// A file of nothing has no Cues and no duration; one of a single picture lasts as long as
// the picture is on screen.
func TestAFinishedFileOfNothing(t *testing.T) {
	key, _ := keyFrame(t)
	empty, disk := create(t, false)
	check(t, empty.Close())
	if f := read(t, disk); len(f.Seeks) != 2 || f.Cues != nil || f.Info.Duration != 0 || f.Size == mkvtest.Unknown {
		t.Fatalf("an empty file of %+v", f)
	}
	still, disk := create(t, false)
	check(t, still.WriteVideo(0, shown, true, key), still.Close())
	if f := read(t, disk); len(f.Cues) != 1 || f.Info.Duration != 40 {
		t.Fatalf("a file of a picture of %+v", f)
	}
}

// After a failed write every call answers the failure, and Close closes the file, which
// holds what was written before it, its places held.
func TestAFailedWrite(t *testing.T) {
	key, video := keyFrame(t)
	if _, err := Create(&mkvtest.Disk{Full: true}, video, true, time.Now()); !errors.Is(err, mkvtest.ErrFull) {
		t.Fatalf("created on a full disk: %v", err)
	}
	w, disk := create(t, true)
	check(t, w.WriteVideo(0, shown, true, key))
	disk.Full = true
	if err := w.WriteVideo(40*ms, shown, false, slice); !errors.Is(err, mkvtest.ErrFull) {
		t.Fatalf("got %v", err)
	}
	disk.Full = false
	for _, err := range []error{w.WriteAudio(60*ms, samples), w.WriteVideo(80*ms, shown, true, key), w.Close()} {
		if !errors.Is(err, mkvtest.ErrFull) {
			t.Fatalf("got %v", err)
		}
	}
	f := read(t, disk)
	if !disk.Closed || len(f.Clusters) != 1 || len(f.Clusters[0].Blocks) != 1 || f.Clusters[0].Size != mkvtest.Unknown || len(f.Seeks) != 2 || f.Info.Duration != 0 || f.Size != mkvtest.Unknown {
		t.Fatalf("a failed file of %+v, closed %v", f, disk.Closed)
	}
}

// A block before the file starts is refused, and the file goes on.
func TestABlockBeforeTheStart(t *testing.T) {
	w, disk := create(t, true)
	if err := w.WriteAudio(-ms, samples); err == nil {
		t.Fatal("a block before the start written")
	}
	check(t, w.WriteAudio(0, samples))
	if clusters := read(t, disk).Clusters; len(clusters) != 1 || clusters[0].Blocks[0].Time != 0 {
		t.Fatalf("clusters %+v", clusters)
	}
}
