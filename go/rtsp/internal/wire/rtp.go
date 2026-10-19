package wire

import (
	"encoding/binary"
	"errors"
	"net"
)

// Packet is an RTP packet (RFC 3550), as far as playing a stream goes.
type Packet struct {
	PayloadType uint8
	// Set on the last packet of what its payload format frames: an access unit's.
	Marker    bool
	Sequence  uint16
	Timestamp uint32
	Payload   []byte
}

// rtpVersion is the version every packet carries, in the top two bits of its first
// byte.
const rtpVersion = 2

var errPacket = errors.New("rtsp: not an RTP packet")

// ParsePacket reads an RTP packet, its payload b's own.
func ParsePacket(b []byte) (Packet, error) {
	if len(b) < 12 || b[0]>>6 != rtpVersion {
		return Packet{}, errPacket
	}
	padding, extension, csrcs := b[0]&0x20 != 0, b[0]&0x10 != 0, int(b[0]&0x0f)
	p := Packet{
		PayloadType: b[1] & 0x7f,
		Marker:      b[1]&0x80 != 0,
		Sequence:    binary.BigEndian.Uint16(b[2:]),
		Timestamp:   binary.BigEndian.Uint32(b[4:]),
	}
	payload := b[12:]
	if len(payload) < 4*csrcs {
		return Packet{}, errPacket
	}
	payload = payload[4*csrcs:]
	if extension {
		if len(payload) < 4 {
			return Packet{}, errPacket
		}
		length := 4 + 4*int(binary.BigEndian.Uint16(payload[2:]))
		if len(payload) < length {
			return Packet{}, errPacket
		}
		payload = payload[length:]
	}
	if padding {
		if len(payload) == 0 || int(payload[len(payload)-1]) > len(payload) {
			return Packet{}, errPacket
		}
		payload = payload[:len(payload)-int(payload[len(payload)-1])]
	}
	p.Payload = payload
	return p, nil
}

// ListenPair listens on a pair of UDP ports at ip, at every address of the host's if nil,
// as RTP and RTCP take them: RTP's even, RTCP's the one after (RFC 3550 11).
func ListenPair(ip net.IP) (rtp, rtcp *net.UDPConn, err error) {
	for range 64 {
		rtp, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip})
		if err != nil {
			return nil, nil, err
		}
		port := rtp.LocalAddr().(*net.UDPAddr).Port
		if port%2 == 0 {
			if rtcp, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: port + 1}); err == nil {
				return rtp, rtcp, nil
			}
		}
		rtp.Close()
	}
	return nil, nil, errors.New("rtsp: no pair of UDP ports free")
}

// Receive reads port until it closes, or take, given each datagram, its own, and its
// sender, says to stop.
func Receive(port *net.UDPConn, take func(datagram []byte, from *net.UDPAddr) bool) {
	buffer := make([]byte, 1<<16)
	for {
		n, from, err := port.ReadFromUDP(buffer)
		if err != nil || !take(append([]byte(nil), buffer[:n]...), from) {
			return
		}
	}
}

// AppendPacket appends p, from a source of ssrc, with no extension and no padding.
func AppendPacket(b []byte, p Packet, ssrc uint32) []byte {
	second := p.PayloadType
	if p.Marker {
		second |= 0x80
	}
	b = append(b, rtpVersion<<6, second)
	b = binary.BigEndian.AppendUint16(b, p.Sequence)
	b = binary.BigEndian.AppendUint32(b, p.Timestamp)
	b = binary.BigEndian.AppendUint32(b, ssrc)
	return append(b, p.Payload...)
}
