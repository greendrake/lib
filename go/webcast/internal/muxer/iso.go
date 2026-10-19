package muxer

import (
	"encoding/binary"
	"math"
)

// The boxes of the ISO base media file format a fragmented MP4 stream is made of: the
// movie's header, a video track's, and each fragment's. They derive from go2rtc's pkg/iso
// and pkg/core.

// codecH265 is the codec a track's sample entry is written for.
const codecH265 = "H265"

type Movie struct {
	b     []byte
	start []int
}

func NewMovie(size int) *Movie {
	return &Movie{b: make([]byte, 0, size)}
}

func (m *Movie) Bytes() []byte {
	return m.b
}

func (m *Movie) StartAtom(name string) {
	m.start = append(m.start, len(m.b))
	m.b = append(m.b, 0, 0, 0, 0)
	m.b = append(m.b, name...)
}

func (m *Movie) EndAtom() {
	n := len(m.start) - 1

	i := m.start[n]
	size := uint32(len(m.b) - i)
	binary.BigEndian.PutUint32(m.b[i:], size)

	m.start = m.start[:n]
}

func (m *Movie) Write(b []byte) {
	m.b = append(m.b, b...)
}

func (m *Movie) WriteBytes(b ...byte) {
	m.b = append(m.b, b...)
}

func (m *Movie) WriteString(s string) {
	m.b = append(m.b, s...)
}

func (m *Movie) Skip(n int) {
	m.b = append(m.b, make([]byte, n)...)
}

func (m *Movie) WriteUint16(v uint16) {
	m.b = append(m.b, byte(v>>8), byte(v))
}

func (m *Movie) WriteUint24(v uint32) {
	m.b = append(m.b, byte(v>>16), byte(v>>8), byte(v))
}

func (m *Movie) WriteUint32(v uint32) {
	m.b = append(m.b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func (m *Movie) WriteUint64(v uint64) {
	m.b = append(m.b, byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func (m *Movie) WriteFloat16(f float64) {
	i, f := math.Modf(f)
	f *= 256
	m.b = append(m.b, byte(i), byte(f))
}

func (m *Movie) WriteFloat32(f float64) {
	i, f := math.Modf(f)
	f *= 65536
	m.b = append(m.b, byte(uint16(i)>>8), byte(i), byte(uint16(f)>>8), byte(f))
}

func (m *Movie) WriteMatrix() {
	m.WriteUint32(0x00010000)
	m.Skip(4)
	m.Skip(4)
	m.Skip(4)
	m.WriteUint32(0x00010000)
	m.Skip(4)
	m.Skip(4)
	m.Skip(4)
	m.WriteUint32(0x40000000)
}

func (m *Movie) WriteVideo(codec string, width, height uint16, conf []byte) {
	// https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap3/qtff3.html
	switch codec {
	case codecH265:
		m.StartAtom("hev1")
	default:
		panic("unsupported iso video: " + codec)
	}
	m.Skip(6)
	m.WriteUint16(1)      // data_reference_index
	m.Skip(2)             // version
	m.Skip(2)             // revision
	m.Skip(4)             // vendor
	m.Skip(4)             // temporal quality
	m.Skip(4)             // spatial quality
	m.WriteUint16(width)  // width
	m.WriteUint16(height) // height
	m.WriteFloat32(72)    // horizontal resolution
	m.WriteFloat32(72)    // vertical resolution
	m.Skip(4)             // reserved
	m.WriteUint16(1)      // frame count
	m.Skip(32)            // compressor name
	m.WriteUint16(24)     // depth
	m.WriteUint16(0xFFFF) // color table id (-1)

	switch codec {
	case codecH265:
		m.StartAtom("hvcC")
	}
	m.Write(conf)
	m.EndAtom() // AVCC

	m.StartAtom("pasp") // Pixel Aspect Ratio
	m.WriteUint32(1)    // hSpacing
	m.WriteUint32(1)    // vSpacing
	m.EndAtom()

	m.EndAtom() // AVC1
}

const (
	Ftyp                        = "ftyp"
	Moov                        = "moov"
	MoovMvhd                    = "mvhd"
	MoovTrak                    = "trak"
	MoovTrakTkhd                = "tkhd"
	MoovTrakMdia                = "mdia"
	MoovTrakMdiaMdhd            = "mdhd"
	MoovTrakMdiaHdlr            = "hdlr"
	MoovTrakMdiaMinf            = "minf"
	MoovTrakMdiaMinfVmhd        = "vmhd"
	MoovTrakMdiaMinfSmhd        = "smhd"
	MoovTrakMdiaMinfDinf        = "dinf"
	MoovTrakMdiaMinfDinfDref    = "dref"
	MoovTrakMdiaMinfDinfDrefUrl = "url "
	MoovTrakMdiaMinfStbl        = "stbl"
	MoovTrakMdiaMinfStblStsd    = "stsd"
	MoovTrakMdiaMinfStblStts    = "stts"
	MoovTrakMdiaMinfStblStsc    = "stsc"
	MoovTrakMdiaMinfStblStsz    = "stsz"
	MoovTrakMdiaMinfStblStco    = "stco"
	MoovMvex                    = "mvex"
	MoovMvexTrex                = "trex"
	Moof                        = "moof"
	MoofMfhd                    = "mfhd"
	MoofTraf                    = "traf"
	MoofTrafTfhd                = "tfhd"
	MoofTrafTfdt                = "tfdt"
	MoofTrafTrun                = "trun"
	Mdat                        = "mdat"
)

const (
	sampleIsNonSync  = 0x10000
	sampleDependsOn1 = 0x1000000
	sampleDependsOn2 = 0x2000000

	SampleVideoIFrame    = sampleDependsOn2
	SampleVideoNonIFrame = sampleDependsOn1 | sampleIsNonSync
	SampleAudio          = sampleDependsOn2 //sampleIsNonSync
)

func (m *Movie) WriteFileType() {
	m.StartAtom(Ftyp)
	m.WriteString("iso5")
	m.WriteUint32(512)
	m.WriteString("iso5")
	m.WriteString("iso6")
	m.WriteString("mp41")
	m.EndAtom()
}

func (m *Movie) WriteMovieHeader() {
	m.StartAtom(MoovMvhd)
	m.Skip(1)           // version
	m.Skip(3)           // flags
	m.Skip(4)           // create time
	m.Skip(4)           // modify time
	m.WriteUint32(1000) // time scale
	m.Skip(4)           // duration
	m.WriteFloat32(1)   // preferred rate
	m.WriteFloat16(1)   // preferred volume
	m.Skip(10)          // reserved
	m.WriteMatrix()
	m.Skip(6 * 4)             // predefined?
	m.WriteUint32(0xFFFFFFFF) // next track ID
	m.EndAtom()
}

func (m *Movie) WriteTrackHeader(id uint32, width, height uint16) {
	const (
		TkhdTrackEnabled   = 0x0001
		TkhdTrackInMovie   = 0x0002
		TkhdTrackInPreview = 0x0004
		TkhdTrackInPoster  = 0x0008
	)

	// https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-32963
	m.StartAtom(MoovTrakTkhd)
	m.Skip(1) // version
	m.WriteUint24(TkhdTrackEnabled | TkhdTrackInMovie)
	m.Skip(4)         // create time
	m.Skip(4)         // modify time
	m.WriteUint32(id) // trackID
	m.Skip(4)         // reserved
	m.Skip(4)         // duration
	m.Skip(8)         // reserved
	m.Skip(2)         // layer
	if width > 0 {
		m.Skip(2)
		m.Skip(2)
	} else {
		m.WriteUint16(1)  // alternate group
		m.WriteFloat16(1) // volume
	}
	m.Skip(2) // reserved
	m.WriteMatrix()
	if width > 0 {
		m.WriteFloat32(float64(width))
		m.WriteFloat32(float64(height))
	} else {
		m.Skip(4)
		m.Skip(4)
	}
	m.EndAtom()
}

func (m *Movie) WriteMediaHeader(timescale uint32) {
	// https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-32999
	m.StartAtom(MoovTrakMdiaMdhd)
	m.Skip(1)                // version
	m.Skip(3)                // flags
	m.Skip(4)                // creation time
	m.Skip(4)                // modification time
	m.WriteUint32(timescale) // timescale
	m.Skip(4)                // duration
	m.WriteUint16(0x55C4)    // language (Unspecified)
	m.Skip(2)                // quality
	m.EndAtom()
}

func (m *Movie) WriteMediaHandler(s, name string) {
	// https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-33004
	m.StartAtom(MoovTrakMdiaHdlr)
	m.Skip(1) // version
	m.Skip(3) // flags
	m.Skip(4)
	m.WriteString(s)    // handler type (4 byte!)
	m.Skip(3 * 4)       // reserved
	m.WriteString(name) // handler name (any len)
	m.Skip(1)           // end string
	m.EndAtom()
}

func (m *Movie) WriteVideoMediaInfo() {
	// https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-33012
	m.StartAtom(MoovTrakMdiaMinfVmhd)
	m.Skip(1)        // version
	m.WriteUint24(1) // flags (You should always set this flag to 1)
	m.Skip(2)        // graphics mode
	m.Skip(3 * 2)    // op color
	m.EndAtom()
}

func (m *Movie) WriteDataInfo() {
	// https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-25680
	m.StartAtom(MoovTrakMdiaMinfDinf)
	m.StartAtom(MoovTrakMdiaMinfDinfDref)
	m.Skip(1)        // version
	m.Skip(3)        // flags
	m.WriteUint32(1) // childrens

	m.StartAtom(MoovTrakMdiaMinfDinfDrefUrl)
	m.Skip(1)        // version
	m.WriteUint24(1) // flags (self reference)
	m.EndAtom()

	m.EndAtom() // DREF
	m.EndAtom() // DINF
}

func (m *Movie) WriteSampleTable(writeSampleDesc func()) {
	// https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-33040
	m.StartAtom(MoovTrakMdiaMinfStbl)

	m.StartAtom(MoovTrakMdiaMinfStblStsd)
	m.Skip(1)        // version
	m.Skip(3)        // flags
	m.WriteUint32(1) // entry count
	writeSampleDesc()
	m.EndAtom()

	m.StartAtom(MoovTrakMdiaMinfStblStts)
	m.Skip(1) // version
	m.Skip(3) // flags
	m.Skip(4) // entry count
	m.EndAtom()

	m.StartAtom(MoovTrakMdiaMinfStblStsc)
	m.Skip(1) // version
	m.Skip(3) // flags
	m.Skip(4) // entry count
	m.EndAtom()

	m.StartAtom(MoovTrakMdiaMinfStblStsz)
	m.Skip(1) // version
	m.Skip(3) // flags
	m.Skip(4) // sample size
	m.Skip(4) // entry count
	m.EndAtom()

	m.StartAtom(MoovTrakMdiaMinfStblStco)
	m.Skip(1) // version
	m.Skip(3) // flags
	m.Skip(4) // entry count
	m.EndAtom()

	m.EndAtom()
}

func (m *Movie) WriteTrackExtend(id uint32) {
	m.StartAtom(MoovMvexTrex)
	m.Skip(1)         // version
	m.Skip(3)         // flags
	m.WriteUint32(id) // trackID
	m.WriteUint32(1)  // default sample description index
	m.Skip(4)         // default sample duration
	m.Skip(4)         // default sample size
	m.Skip(4)         // default sample flags
	m.EndAtom()
}

func (m *Movie) WriteVideoTrack(id uint32, codec string, timescale uint32, width, height uint16, conf []byte) {
	m.StartAtom(MoovTrak)
	m.WriteTrackHeader(id, width, height)

	m.StartAtom(MoovTrakMdia)
	m.WriteMediaHeader(timescale)
	m.WriteMediaHandler("vide", "VideoHandler")

	m.StartAtom(MoovTrakMdiaMinf)
	m.WriteVideoMediaInfo()
	m.WriteDataInfo()
	m.WriteSampleTable(func() {
		m.WriteVideo(codec, width, height, conf)
	})
	m.EndAtom() // MINF

	m.EndAtom() // MDIA
	m.EndAtom() // TRAK
}

const (
	TfhdDefaultSampleDuration = 0x000008
	TfhdDefaultSampleSize     = 0x000010
	TfhdDefaultSampleFlags    = 0x000020
	TfhdDefaultBaseIsMoof     = 0x020000
)

const (
	TrunDataOffset       = 0x000001
	TrunFirstSampleFlags = 0x000004
	TrunSampleDuration   = 0x0000100
	TrunSampleSize       = 0x0000200
	TrunSampleFlags      = 0x0000400
	TrunSampleCTS        = 0x0000800
)

func (m *Movie) WriteMovieFragment(seq, tid, duration, size, flags uint32, dts uint64, cts uint32) {
	m.StartAtom(Moof)

	m.StartAtom(MoofMfhd)
	m.Skip(1)          // version
	m.Skip(3)          // flags
	m.WriteUint32(seq) // sequence number
	m.EndAtom()

	m.StartAtom(MoofTraf)

	m.StartAtom(MoofTrafTfhd)
	m.Skip(1) // version
	m.WriteUint24(
		TfhdDefaultSampleDuration |
			TfhdDefaultSampleSize |
			TfhdDefaultSampleFlags |
			TfhdDefaultBaseIsMoof,
	)
	m.WriteUint32(tid)      // track id
	m.WriteUint32(duration) // default sample duration
	m.WriteUint32(size)     // default sample size
	m.WriteUint32(flags)    // default sample flags
	m.EndAtom()

	m.StartAtom(MoofTrafTfdt)
	m.WriteBytes(1)    // version
	m.Skip(3)          // flags
	m.WriteUint64(dts) // base media decode time
	m.EndAtom()

	m.StartAtom(MoofTrafTrun)
	m.Skip(1) // version

	if cts == 0 {
		m.WriteUint24(TrunDataOffset) // flags
		m.WriteUint32(1)              // sample count

		// data offset: current pos + uint32 len + MDAT header len
		m.WriteUint32(uint32(len(m.b)) + 4 + 8)
	} else {
		m.WriteUint24(TrunDataOffset | TrunSampleCTS)
		m.WriteUint32(1)

		// data offset: current pos + uint32 len + CTS + MDAT header len
		m.WriteUint32(uint32(len(m.b)) + 4 + 4 + 8)
		m.WriteUint32(cts)
	}

	m.EndAtom() // TRUN

	m.EndAtom() // TRAF

	m.EndAtom() // MOOF
}

func (m *Movie) WriteData(b []byte) {
	m.StartAtom(Mdat)
	m.Write(b)
	m.EndAtom()
}
