package mkvtest

import (
	"slices"
	"testing"

	"greendrake.nz/go/ipcam/internal/ebml"
)

// file is a file of a Segment of known size holding segment.
func file(segment ...[]byte) []byte {
	head := ebml.AppendElement(nil, ebml.EBML, ebml.AppendString(nil, ebml.DocType, "matroska"))
	return ebml.AppendElement(head, ebml.Segment, slices.Concat(segment...))
}

// cluster is a Cluster at time 0 of a block of track 1, of unknown size where unknown says.
func cluster(unknown bool) []byte {
	b := ebml.AppendUint(nil, ebml.Timestamp, 0)
	b = ebml.AppendElement(b, ebml.SimpleBlock, []byte{0x81, 0, 0, 0x80, 0xaf})
	if unknown {
		return slices.Concat(ebml.AppendID(nil, ebml.Cluster), ebml.UnknownSize, b)
	}
	return ebml.AppendElement(nil, ebml.Cluster, b)
}

// seekHead is a SeekHead naming the element of id at position.
func seekHead(id uint32, position uint64) []byte {
	b := ebml.AppendElement(nil, ebml.SeekID, ebml.AppendID(nil, id))
	return ebml.AppendElement(nil, ebml.SeekHead, ebml.AppendElement(nil, ebml.Seek, ebml.AppendUint(b, ebml.SeekPosition, position)))
}

// Elements of unknown size run to the next that is not their child, or to the file's end.
func TestUnknownSizes(t *testing.T) {
	b := slices.Concat(ebml.AppendElement(nil, ebml.EBML, nil), ebml.AppendID(nil, ebml.Segment), ebml.UnknownSize, cluster(true), cluster(true))
	f, err := Read(b)
	if err != nil {
		t.Fatal(err)
	}
	if f.Size != Unknown || len(f.Clusters) != 2 || f.Clusters[0].Size != Unknown || len(f.Clusters[0].Blocks) != 1 || f.Clusters[1].Position != int64(len(cluster(true))) {
		t.Fatalf("read %+v", f)
	}
}

// What is not where the sizes and the schema say is refused.
func TestMisplaced(t *testing.T) {
	sought := seekHead(ebml.Cluster, uint64(len(seekHead(ebml.Cluster, 0))))
	if _, err := Read(file(sought, cluster(false))); err != nil {
		t.Fatalf("a well-formed file: %v", err)
	}
	whole := file(cluster(false))
	short := file(cluster(false))
	// The Cluster says it is a byte shorter than it is.
	short[len(short)-len(cluster(false))+4]--
	for what, b := range map[string][]byte{
		"an element past the file's end":            whole[:len(whole)-1],
		"an element past its parent's end":          short,
		"an element out of its parent":              file(ebml.AppendElement(nil, ebml.SimpleBlock, []byte{0x81, 0, 0, 0})),
		"an element at the top level out of place":  append(file(), cluster(false)...),
		"an element of no place in a file":          file(ebml.AppendElement(nil, 0x81, nil)),
		"a leaf of unknown size":                    file(ebml.AppendID(nil, ebml.Info), ebml.UnknownSize, ebml.AppendID(nil, ebml.Duration), ebml.UnknownSize),
		"a float of 4 bytes":                        file(ebml.AppendElement(nil, ebml.Info, ebml.AppendElement(nil, ebml.Duration, make([]byte, 4)))),
		"a laced block":                             file(ebml.AppendElement(nil, ebml.Cluster, ebml.AppendElement(nil, ebml.SimpleBlock, []byte{0x81, 0, 0, 0x02}))),
		"a SeekID of no ID":                         file(ebml.AppendElement(nil, ebml.SeekHead, ebml.AppendElement(nil, ebml.Seek, ebml.AppendElement(nil, ebml.SeekID, []byte{0x1f, 0x43})))),
		"a SeekHead naming what is not where it is": file(seekHead(ebml.Cluster, 0), cluster(false)),
		"no Segment":                                ebml.AppendElement(nil, ebml.EBML, nil),
		"no ID":                                     append(file(), 0),
		"no size":                                   append(file(), 0xec),
	} {
		if _, err := Read(b); err == nil {
			t.Errorf("%s read", what)
		}
	}
}
