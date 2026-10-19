// Package mkvtest reads back, for tests, the files package mkv writes, and has a file in
// memory to write them to.
package mkvtest

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"greendrake.nz/go/ipcam/internal/ebml"
)

// Unknown is the size of an element whose size is not known.
const Unknown = -1

// File is what a file holds, as far as it is written.
type File struct {
	DocType string
	// The Segment's data size: Unknown until the file is finished.
	Size   int64
	Info   Info
	Tracks []Track
	// Where the SeekHead has each element it names, by the element's ID, in the Segment's
	// data: each checked to be there.
	Seeks    map[uint32]int64
	Clusters []Cluster
	Cues     []CuePoint
}

// Info is a file's Info.
type Info struct {
	TimestampScale        uint64
	MuxingApp, WritingApp string
	Date                  time.Time
	// The duration in ms, 0 where the file has none.
	Duration float64
}

// Track is a TrackEntry.
type Track struct {
	Number, UID, Type uint64
	CodecID           string
	CodecPrivate      []byte
	// A video track's picture size.
	Width, Height uint64
	// An audio track's sampling.
	SamplingFrequency float64
	Channels          uint64
}

// Cluster is a Cluster and its blocks.
type Cluster struct {
	// Where the Cluster is in the Segment's data, which a CuePoint points to.
	Position int64
	// Its data size: Unknown while it is the one being written.
	Size int64
	// Its time, in ms.
	Time   uint64
	Blocks []Block
}

// Block is a SimpleBlock.
type Block struct {
	Track uint64
	// Its time, in ms, and that relative to its Cluster's.
	Time     int64
	Relative int16
	Key      bool
	Data     []byte
}

// CuePoint is a CuePoint of a single track.
type CuePoint struct {
	// The time, in ms, and where its Cluster is in the Segment's data.
	Time     uint64
	Track    uint64
	Position int64
}

// kind is what an element's data is.
type kind int

const (
	master kind = iota
	uinteger
	float
	date
	identifier
	block
	// Any bytes: a string's, or binary.
	raw
)

// place is where an element goes, under its parent (0 for the file's top level), and what
// its data is.
type place struct {
	parent uint32
	kind   kind
}

// schema is every element a file has. A Void goes in any master element.
var schema = map[uint32]place{
	ebml.EBML:               {0, master},
	ebml.EBMLVersion:        {ebml.EBML, uinteger},
	ebml.EBMLReadVersion:    {ebml.EBML, uinteger},
	ebml.EBMLMaxIDLength:    {ebml.EBML, uinteger},
	ebml.EBMLMaxSizeLength:  {ebml.EBML, uinteger},
	ebml.DocType:            {ebml.EBML, raw},
	ebml.DocTypeVersion:     {ebml.EBML, uinteger},
	ebml.DocTypeReadVersion: {ebml.EBML, uinteger},
	ebml.Void:               {0, raw},
	ebml.Segment:            {0, master},
	ebml.SeekHead:           {ebml.Segment, master},
	ebml.Seek:               {ebml.SeekHead, master},
	ebml.SeekID:             {ebml.Seek, identifier},
	ebml.SeekPosition:       {ebml.Seek, uinteger},
	ebml.Info:               {ebml.Segment, master},
	ebml.TimestampScale:     {ebml.Info, uinteger},
	ebml.Duration:           {ebml.Info, float},
	ebml.DateUTC:            {ebml.Info, date},
	ebml.MuxingApp:          {ebml.Info, raw},
	ebml.WritingApp:         {ebml.Info, raw},
	ebml.Tracks:             {ebml.Segment, master},
	ebml.TrackEntry:         {ebml.Tracks, master},
	ebml.TrackNumber:        {ebml.TrackEntry, uinteger},
	ebml.TrackUID:           {ebml.TrackEntry, uinteger},
	ebml.TrackType:          {ebml.TrackEntry, uinteger},
	ebml.CodecID:            {ebml.TrackEntry, raw},
	ebml.CodecPrivate:       {ebml.TrackEntry, raw},
	ebml.Video:              {ebml.TrackEntry, master},
	ebml.PixelWidth:         {ebml.Video, uinteger},
	ebml.PixelHeight:        {ebml.Video, uinteger},
	ebml.Audio:              {ebml.TrackEntry, master},
	ebml.SamplingFrequency:  {ebml.Audio, float},
	ebml.Channels:           {ebml.Audio, uinteger},
	ebml.Cluster:            {ebml.Segment, master},
	ebml.Timestamp:          {ebml.Cluster, uinteger},
	ebml.SimpleBlock:        {ebml.Cluster, block},
	ebml.Cues:               {ebml.Segment, master},
	ebml.CuePoint:           {ebml.Cues, master},
	ebml.CueTime:            {ebml.CuePoint, uinteger},
	ebml.CueTrackPositions:  {ebml.CuePoint, master},
	ebml.CueTrack:           {ebml.CueTrackPositions, uinteger},
	ebml.CueClusterPosition: {ebml.CueTrackPositions, uinteger},
}

// element is an element read: where it and its data start in the file, its ID and data
// size (Unknown where it is not known), and its data or, a master element's, its children.
type element struct {
	at, start int64
	id        uint32
	size      int64
	data      []byte
	children  []element
}

// all are e's children of id.
func (e element) all(id uint32) []element {
	var all []element
	for _, child := range e.children {
		if child.id == id {
			all = append(all, child)
		}
	}
	return all
}

// fields are e's children by their IDs.
func (e element) fields() map[uint32]element {
	fields := map[uint32]element{}
	for _, child := range e.children {
		fields[child.id] = child
	}
	return fields
}

// Read reads a file as far as it is written. It walks the elements by the sizes it finds:
// one of a known size ends exactly where the next starts or the file does, and one of
// unknown size runs to the next element that is not its child, or to the file's end.
// Every element has its place, in the schema, and its data is of its kind.
func Read(b []byte) (File, error) {
	top, n, err := walk(b, 0, 0)
	if err != nil {
		return File{}, err
	}
	if n != len(b) {
		return File{}, fmt.Errorf("mkvtest: an element out of place at %d", n)
	}
	if len(top) != 2 || top[0].id != ebml.EBML || top[1].id != ebml.Segment {
		return File{}, errors.New("mkvtest: a file not of an EBML header and a Segment")
	}
	segment := top[1]
	f := File{DocType: string(top[0].fields()[ebml.DocType].data), Size: segment.size, Seeks: map[uint32]int64{}}
	for _, e := range segment.children {
		fields := e.fields()
		switch e.id {
		case ebml.SeekHead:
			for _, s := range e.all(ebml.Seek) {
				fields := s.fields()
				f.Seeks[uint32(ebml.Uint(fields[ebml.SeekID].data))] = int64(ebml.Uint(fields[ebml.SeekPosition].data))
			}
		case ebml.Info:
			f.Info = Info{
				TimestampScale: ebml.Uint(fields[ebml.TimestampScale].data),
				MuxingApp:      string(fields[ebml.MuxingApp].data),
				WritingApp:     string(fields[ebml.WritingApp].data),
				Date:           ebml.Epoch.Add(time.Duration(ebml.Uint(fields[ebml.DateUTC].data))),
				Duration:       math.Float64frombits(ebml.Uint(fields[ebml.Duration].data)),
			}
		case ebml.Tracks:
			for _, entry := range e.all(ebml.TrackEntry) {
				fields := entry.fields()
				video, audio := fields[ebml.Video].fields(), fields[ebml.Audio].fields()
				f.Tracks = append(f.Tracks, Track{
					Number:            ebml.Uint(fields[ebml.TrackNumber].data),
					UID:               ebml.Uint(fields[ebml.TrackUID].data),
					Type:              ebml.Uint(fields[ebml.TrackType].data),
					CodecID:           string(fields[ebml.CodecID].data),
					CodecPrivate:      fields[ebml.CodecPrivate].data,
					Width:             ebml.Uint(video[ebml.PixelWidth].data),
					Height:            ebml.Uint(video[ebml.PixelHeight].data),
					SamplingFrequency: math.Float64frombits(ebml.Uint(audio[ebml.SamplingFrequency].data)),
					Channels:          ebml.Uint(audio[ebml.Channels].data),
				})
			}
		case ebml.Cluster:
			c := Cluster{Position: e.at - segment.start, Size: e.size, Time: ebml.Uint(fields[ebml.Timestamp].data)}
			for _, simple := range e.all(ebml.SimpleBlock) {
				d := simple.data
				relative := int16(binary.BigEndian.Uint16(d[1:3]))
				c.Blocks = append(c.Blocks, Block{Track: uint64(d[0] & 0x7f), Time: int64(c.Time) + int64(relative), Relative: relative, Key: d[3]&0x80 != 0, Data: d[4:]})
			}
			f.Clusters = append(f.Clusters, c)
		case ebml.Cues:
			for _, point := range e.all(ebml.CuePoint) {
				fields := point.fields()
				positions := fields[ebml.CueTrackPositions].fields()
				f.Cues = append(f.Cues, CuePoint{
					Time:     ebml.Uint(fields[ebml.CueTime].data),
					Track:    ebml.Uint(positions[ebml.CueTrack].data),
					Position: int64(ebml.Uint(positions[ebml.CueClusterPosition].data)),
				})
			}
		}
	}
	for id, position := range f.Seeks {
		if !slices.ContainsFunc(segment.children, func(e element) bool { return e.id == id && e.at-segment.start == position }) {
			return File{}, fmt.Errorf("mkvtest: the SeekHead has %x at %d, where it is not", id, position)
		}
	}
	return f, nil
}

// walk reads off b, which starts at at in the file, the elements of parent, as far as one
// that is not parent's: the elements, and how many bytes they take.
func walk(b []byte, at int64, parent uint32) ([]element, int, error) {
	var elements []element
	i := 0
	for i < len(b) {
		id, idSize, err := ebml.ReadID(b[i:])
		if err != nil {
			return nil, 0, fmt.Errorf("mkvtest: at %d: %w", at+int64(i), err)
		}
		p, known := schema[id]
		if !known {
			return nil, 0, fmt.Errorf("mkvtest: an element %x at %d, of no place in a file", id, at+int64(i))
		}
		if id != ebml.Void && p.parent != parent {
			return elements, i, nil
		}
		size, unknown, sizeSize, err := ebml.ReadSize(b[i+idSize:])
		if err != nil {
			return nil, 0, fmt.Errorf("mkvtest: %x at %d: %w", id, at+int64(i), err)
		}
		start := i + idSize + sizeSize
		e := element{at: at + int64(i), start: at + int64(start), id: id, size: int64(size)}
		switch {
		case unknown && p.kind != master:
			return nil, 0, fmt.Errorf("mkvtest: %x at %d of unknown size", id, e.at)
		case unknown:
			e.size = Unknown
			var n int
			if e.children, n, err = walk(b[start:], e.start, id); err != nil {
				return nil, 0, err
			}
			i = start + n
		case size > uint64(len(b)-start):
			return nil, 0, fmt.Errorf("mkvtest: %x at %d of %d bytes runs past where it can end, at %d", id, e.at, size, at+int64(len(b)))
		case p.kind == master:
			i = start + int(size)
			var n int
			if e.children, n, err = walk(b[start:i], e.start, id); err != nil {
				return nil, 0, err
			}
			if n != int(size) {
				return nil, 0, fmt.Errorf("mkvtest: an element out of place in %x at %d", id, e.start+int64(n))
			}
		default:
			i = start + int(size)
			e.data = b[start:i]
			if err := check(p.kind, e.data); err != nil {
				return nil, 0, fmt.Errorf("mkvtest: %x at %d: %w", id, e.at, err)
			}
		}
		elements = append(elements, e)
	}
	return elements, i, nil
}

// check checks that data is of kind.
func check(k kind, data []byte) error {
	switch {
	case k == uinteger && len(data) > 8, (k == float || k == date) && len(data) != 8:
		return fmt.Errorf("of %d bytes", len(data))
	case k == identifier:
		if _, n, err := ebml.ReadID(data); err != nil || n != len(data) {
			return errors.New("not an ID")
		}
	case k == block && (len(data) < 4 || data[0]&0x80 == 0 || data[3]&0x06 != 0):
		return errors.New("not a block of a 1-byte track number and no lacing")
	}
	return nil
}
