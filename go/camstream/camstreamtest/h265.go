package camstreamtest

import (
	"github.com/bluenviron/mediacommon/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/pkg/codecs/h265"
)

// NALUnit is an H.265 NAL unit of type kind: its header, and a byte of payload.
func NALUnit(kind h265.NALUType) []byte {
	return []byte{byte(kind) << 1, 1, 0xaf}
}

// AnnexB is the access unit of nalus, in Annex-B.
func AnnexB(nalus ...[]byte) []byte {
	b, err := h264.AnnexB(nalus).Marshal()
	if err != nil {
		panic(err)
	}
	return b
}
