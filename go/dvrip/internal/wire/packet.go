// Package wire is what both ends of the protocol share: the packets every message
// travels in, the message codes, the JSON bodies' framing, the password hash, the
// forms configuration values take and laying one configuration over another, and
// cutting a media stream into frames.
package wire

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// HeaderSize is the size of the header in front of every message, control and media
// alike.
const HeaderSize = 20

// MaxPacketData bounds the data one packet may announce. Devices keep packets to 16 KB,
// 32 KB at most; a length anywhere near this bound means the stream has lost its place,
// and is refused before it costs an allocation.
const MaxPacketData = 1 << 20

// Code is the header's Message Id: what a packet is. A reply's is its request's plus
// one.
type Code uint16

const (
	Login      Code = 1000
	KeepAlive  Code = 1006
	SystemInfo Code = 1020
	ConfigSet  Code = 1040
	ConfigGet  Code = 1042
	// The channel title as a bitmap to draw, its body binary: see EncodeBitmap.
	TitleBitmapSet Code = 1050
	Ability        Code = 1360
	Monitor        Code = 1410
	MonitorData    Code = 1412
	MonitorClaim   Code = 1413
	SystemManager  Code = 1450
	TimeQuery      Code = 1452
	ConfigExport   Code = 1542
)

// Header is what a packet's header says, less its head flag, version and length.
type Header struct {
	Session  uint32
	Sequence uint32
	// Control packets: how many packets the message takes, and this one's number from
	// 0; 0 and 1 both mean a single packet. Media packets put the channel and the end
	// of data flag here instead, which nothing reads.
	Total, Current uint8
	Code           Code
}

// Packet is one header and the data it announces.
type Packet struct {
	Header
	Data []byte
}

func ReadPacket(r io.Reader) (Packet, error) {
	var h [HeaderSize]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Packet{}, err
	}
	if h[0] != 0xFF {
		return Packet{}, fmt.Errorf("dvrip: packet head flag %#02x, want 0xff", h[0])
	}
	n := binary.LittleEndian.Uint32(h[16:])
	if n > MaxPacketData {
		return Packet{}, fmt.Errorf("dvrip: packet announces %d bytes", n)
	}
	p := Packet{
		Header: Header{
			Session:  binary.LittleEndian.Uint32(h[4:]),
			Sequence: binary.LittleEndian.Uint32(h[8:]),
			Total:    h[12],
			Current:  h[13],
			Code:     Code(binary.LittleEndian.Uint16(h[14:])),
		},
		Data: make([]byte, n),
	}
	if _, err := io.ReadFull(r, p.Data); err != nil {
		return Packet{}, err
	}
	return p, nil
}

// ReadMessage reads one control message, joining the packets of one too big for a
// packet.
func ReadMessage(r io.Reader) (Packet, error) {
	p, err := ReadPacket(r)
	if err != nil || p.Total <= 1 {
		return p, err
	}
	if p.Current != 0 {
		return Packet{}, fmt.Errorf("dvrip: message starts at packet %d of %d", p.Current, p.Total)
	}
	for i := uint8(1); i < p.Total; i++ {
		next, err := ReadPacket(r)
		if err != nil {
			return Packet{}, err
		}
		if next.Code != p.Code || next.Current != i {
			return Packet{}, fmt.Errorf("dvrip: message %d packet %d of %d interrupted by message %d packet %d", p.Code, i, p.Total, next.Code, next.Current)
		}
		p.Data = append(p.Data, next.Data...)
	}
	return p, nil
}

// AppendPacket appends one packet to dst. The version byte is 0, which is what every
// client known to work sends; the vendor's document says 1, and devices take either.
func AppendPacket(dst []byte, h Header, data []byte) []byte {
	var b [HeaderSize]byte
	b[0] = 0xFF
	binary.LittleEndian.PutUint32(b[4:], h.Session)
	binary.LittleEndian.PutUint32(b[8:], h.Sequence)
	b[12] = h.Total
	b[13] = h.Current
	binary.LittleEndian.PutUint16(b[14:], uint16(h.Code))
	binary.LittleEndian.PutUint32(b[16:], uint32(len(data)))
	return append(append(dst, b[:]...), data...)
}

// jsonTrailer ends every JSON body, both ways.
var jsonTrailer = []byte{'\n', 0}

// EncodeJSON encodes v as a body.
func EncodeJSON(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append(data, jsonTrailer...), nil
}

// TrimJSON strips what the other end appends to a JSON body: the trailer, which some
// firmwares shorten to the newline alone.
func TrimJSON(data []byte) []byte {
	return bytes.TrimRight(data, "\x00\n\r\t ")
}

// SofiaHash is the form a password takes on the wire: its MD5 digest, read as eight
// byte pairs, each pair's sum indexing a 62-character alphabet.
func SofiaHash(password string) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	digest := md5.Sum([]byte(password))
	hash := make([]byte, 0, len(digest)/2)
	for i := 0; i < len(digest); i += 2 {
		hash = append(hash, alphabet[(int(digest[i])+int(digest[i+1]))%len(alphabet)])
	}
	return string(hash)
}
