// Package mkv writes a recording to a Matroska file (RFC 9559): one H.265 track and, where
// the stream carries audio, one of G.711 A-law at 8 kHz, mono. Each block goes to the file
// as it is written, and what is known only at close is written over places held for it,
// so that a file cut short plays as far as it was written.
package mkv

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"time"

	"github.com/bluenviron/mediacommon/pkg/codecs/h264"
	"greendrake.nz/go/camstream/h265"
	"greendrake.nz/go/ipcam/internal/ebml"
)

// AudioRate is the audio track's sample rate: G.711 A-law at 8 kHz is the one audio format
// the cameras' stream format defines.
const AudioRate = 8000

// The tracks, by their numbers, which are their UIDs too, and the types of track.
const (
	videoTrack = 1
	audioTrack = 2

	videoType = 1
	audioType = 2
)

// app is the muxing and writing application a file names.
const app = "greendrake.nz/go/ipcam/record/mkv"

// WriteSeekCloser is what a file is written through: the standard library has no such
// interface.
type WriteSeekCloser interface {
	io.Writer
	io.Seeker
	io.Closer
}

// header is every file's EBML header.
var header = func() []byte {
	var b []byte
	b = ebml.AppendUint(b, ebml.EBMLVersion, 1)
	b = ebml.AppendUint(b, ebml.EBMLReadVersion, 1)
	b = ebml.AppendUint(b, ebml.EBMLMaxIDLength, 4)
	b = ebml.AppendUint(b, ebml.EBMLMaxSizeLength, 8)
	b = ebml.AppendString(b, ebml.DocType, "matroska")
	b = ebml.AppendUint(b, ebml.DocTypeVersion, 4)
	b = ebml.AppendUint(b, ebml.DocTypeReadVersion, 2)
	return ebml.AppendElement(nil, ebml.EBML, b)
}()

// audioEntry is the audio track's TrackEntry, its format a WAVEFORMATEX: A-law, mono, at
// AudioRate, a byte a sample.
var audioEntry = func() []byte {
	format := binary.LittleEndian.AppendUint16(nil, 6) // WAVE_FORMAT_ALAW
	format = binary.LittleEndian.AppendUint16(format, 1)
	format = binary.LittleEndian.AppendUint32(format, AudioRate)
	format = binary.LittleEndian.AppendUint32(format, AudioRate) // bytes a second
	format = binary.LittleEndian.AppendUint16(format, 1)         // block align
	format = binary.LittleEndian.AppendUint16(format, 8)         // bits a sample
	format = binary.LittleEndian.AppendUint16(format, 0)         // no extra format bytes
	sound := ebml.AppendFloat(nil, ebml.SamplingFrequency, AudioRate)
	sound = ebml.AppendUint(sound, ebml.Channels, 1)
	return trackEntry(audioTrack, audioType, "A_MS/ACM", format, ebml.AppendElement(nil, ebml.Audio, sound))
}()

// durationVoid holds the place of the Info's Duration until close.
var durationVoid = ebml.AppendVoid(nil, len(ebml.AppendFloat(nil, ebml.Duration, 0)))

// cuesVoid holds the place of the SeekHead's entry for the Cues until close.
var cuesVoid = ebml.AppendVoid(nil, len(seek(ebml.Cues, 0)))

// trackEntry is the TrackEntry of track number, of type kind, with its codec, its codec's
// private data and media, its Video or Audio element.
func trackEntry(number, kind uint64, codec string, private, media []byte) []byte {
	var b []byte
	b = ebml.AppendUint(b, ebml.TrackNumber, number)
	b = ebml.AppendUint(b, ebml.TrackUID, number)
	b = ebml.AppendUint(b, ebml.TrackType, kind)
	b = ebml.AppendString(b, ebml.CodecID, codec)
	b = ebml.AppendElement(b, ebml.CodecPrivate, private)
	return ebml.AppendElement(nil, ebml.TrackEntry, append(b, media...))
}

// seek is the SeekHead's entry for the element of id at position in the Segment. Its
// position takes 8 bytes, so that every entry is of the same size.
func seek(id uint32, position int64) []byte {
	b := ebml.AppendElement(nil, ebml.SeekID, ebml.AppendID(nil, id))
	b = ebml.AppendUint8(b, ebml.SeekPosition, uint64(position))
	return ebml.AppendElement(nil, ebml.Seek, b)
}

// Writer writes a file.
type Writer struct {
	f     WriteSeekCloser
	audio bool
	// How much of the file is written: where the next element goes.
	end int64
	// Where the Segment's data starts, which positions in it count from.
	segment int64
	// Where the fields known only at close go: the Segment's size, the SeekHead's entry for
	// the Cues and the Duration.
	segmentSize, cuesSeek, duration int64
	// Where the open Cluster's size is, 0 while none is open, and its time in ms.
	clusterSize, clusterTime int64
	// A CuePoint for each Cluster a key frame opened.
	cuePoints []byte
	// When the block that ends last ends: the file's duration.
	until time.Duration
	// The first failure to write, which every call after it answers: what a failed write
	// left of the file is not known.
	err error
}

// Create writes the head of a file to f: its video described by video, with an audio
// track if audio says so, the file made at date. On failure, f is the caller's to close.
func Create(f WriteSeekCloser, video h265.Decoding, audio bool, date time.Time) (*Writer, error) {
	var info []byte
	info = ebml.AppendUint(info, ebml.TimestampScale, uint64(time.Millisecond))
	info = ebml.AppendString(info, ebml.MuxingApp, app)
	info = ebml.AppendString(info, ebml.WritingApp, app)
	info = ebml.AppendDate(info, ebml.DateUTC, date)
	info = ebml.AppendElement(nil, ebml.Info, append(info, durationVoid...))

	picture := ebml.AppendUint(nil, ebml.PixelWidth, uint64(video.Width))
	picture = ebml.AppendUint(picture, ebml.PixelHeight, uint64(video.Height))
	tracks := trackEntry(videoTrack, videoType, "V_MPEGH/ISO/HEVC", video.Config, ebml.AppendElement(nil, ebml.Video, picture))
	if audio {
		tracks = append(tracks, audioEntry...)
	}
	tracks = ebml.AppendElement(nil, ebml.Tracks, tracks)

	// The SeekHead's size does not depend on the positions it holds.
	seekHead := func(at int64) []byte {
		entries := slices.Concat(seek(ebml.Info, at), seek(ebml.Tracks, at+int64(len(info))), cuesVoid)
		return ebml.AppendElement(nil, ebml.SeekHead, entries)
	}
	head := seekHead(int64(len(seekHead(0))))

	w := &Writer{f: f, audio: audio, segmentSize: int64(len(header)) + 4}
	w.segment = w.segmentSize + 8
	w.cuesSeek = w.segment + int64(len(head)-len(cuesVoid))
	w.duration = w.segment + int64(len(head)+len(info)-len(durationVoid))
	w.write(slices.Concat(header, ebml.AppendID(nil, ebml.Segment), ebml.UnknownSize, head, info, tracks))
	if w.err != nil {
		return nil, w.err
	}
	return w, nil
}

// WriteVideo writes an access unit at at, on screen for duration, its NAL units framed as
// the decoder configuration says: behind 4-byte lengths. A key frame opens a Cluster,
// which a CuePoint points to.
func (w *Writer) WriteVideo(at, duration time.Duration, keyframe bool, nalus [][]byte) error {
	payload, err := h264.AVCC(nalus).Marshal()
	if err != nil {
		return err
	}
	return w.block(videoTrack, at, duration, keyframe, payload)
}

// WriteAudio writes A-law samples at at.
func (w *Writer) WriteAudio(at time.Duration, samples []byte) error {
	if !w.audio {
		return errors.New("mkv: the file has no audio track")
	}
	return w.block(audioTrack, at, time.Duration(len(samples))*time.Second/AudioRate, true, samples)
}

// block writes a SimpleBlock of track at at, lasting duration, key or not. A Cluster opens
// for it at a video key frame, and wherever its time relative to the open Cluster's would
// leave the 16 bits it has: the time of a block behind its Cluster's is negative.
func (w *Writer) block(track byte, at, duration time.Duration, key bool, payload []byte) error {
	if w.err != nil {
		return w.err
	}
	if at < 0 {
		return fmt.Errorf("mkv: a block at %v, before the file starts", at)
	}
	t := at.Milliseconds()
	relative := t - w.clusterTime
	cue := track == videoTrack && key
	var b []byte
	if cue || w.clusterSize == 0 || relative < math.MinInt16 || relative > math.MaxInt16 {
		w.finishCluster()
		// A Cluster opened at no key frame has no CuePoint: a seek to it would land on a
		// picture that cannot be decoded.
		if cue {
			positions := ebml.AppendUint(nil, ebml.CueTrack, videoTrack)
			positions = ebml.AppendUint(positions, ebml.CueClusterPosition, uint64(w.end-w.segment))
			point := ebml.AppendUint(nil, ebml.CueTime, uint64(t))
			w.cuePoints = ebml.AppendElement(w.cuePoints, ebml.CuePoint, ebml.AppendElement(point, ebml.CueTrackPositions, positions))
		}
		w.clusterSize, w.clusterTime, relative = w.end+4, t, 0
		b = append(ebml.AppendID(b, ebml.Cluster), ebml.UnknownSize...)
		b = ebml.AppendUint(b, ebml.Timestamp, uint64(t))
	}
	var flags byte
	if key {
		flags = 0x80
	}
	b = ebml.AppendID(b, ebml.SimpleBlock)
	b = ebml.AppendSize(b, uint64(4+len(payload)))
	b = append(b, 0x80|track, byte(relative>>8), byte(relative), flags)
	w.write(append(b, payload...))
	w.until = max(w.until, at+duration)
	return w.err
}

// Close finishes the file and closes it: the open Cluster's size, the Cues and the
// SeekHead's entry for them, the Duration, and the Segment's size. A writer that has failed
// closes the file as it is, and answers its failure.
func (w *Writer) Close() error {
	if w.err == nil {
		w.finishCluster()
		if w.cuePoints != nil {
			at := w.end
			w.write(ebml.AppendElement(nil, ebml.Cues, w.cuePoints))
			w.patch(w.cuesSeek, seek(ebml.Cues, at-w.segment))
		}
		// A Duration is more than nothing.
		if w.until > 0 {
			w.patch(w.duration, ebml.AppendFloat(nil, ebml.Duration, float64(w.until)/float64(time.Millisecond)))
		}
		w.patch(w.segmentSize, ebml.AppendSize8(nil, uint64(w.end-w.segment)))
	}
	return errors.Join(w.err, w.f.Close())
}

// finishCluster writes the open Cluster's size, if one is open.
func (w *Writer) finishCluster() {
	if w.clusterSize != 0 {
		w.patch(w.clusterSize, ebml.AppendSize8(nil, uint64(w.end-w.clusterSize-8)))
	}
}

// write appends b to the file, unless the writer has failed.
func (w *Writer) write(b []byte) {
	if w.err != nil {
		return
	}
	var n int
	n, w.err = w.f.Write(b)
	w.end += int64(n)
}

// patch writes b over the file at at and goes back to its end, unless the writer has
// failed.
func (w *Writer) patch(at int64, b []byte) {
	if w.err != nil {
		return
	}
	if _, w.err = w.f.Seek(at, io.SeekStart); w.err != nil {
		return
	}
	if _, w.err = w.f.Write(b); w.err != nil {
		return
	}
	_, w.err = w.f.Seek(w.end, io.SeekStart)
}
