// Package wire is RTSP 1.0 on a TCP connection: its messages, the RTP packets
// interleaved with them, and Digest authentication, for the client and the test server
// alike.
package wire

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Version is the protocol's, on every message's first line.
const Version = "RTSP/1.0"

// Header is a message's fields, in order, their names as written: some servers match
// names case and all, CSeq above all.
type Header [][2]string

// Get is the first value of the field name names, in any case.
func (h Header) Get(name string) string {
	for _, field := range h {
		if strings.EqualFold(field[0], name) {
			return field[1]
		}
	}
	return ""
}

// Values are every value of the field name names, in any case.
func (h Header) Values(name string) []string {
	var values []string
	for _, field := range h {
		if strings.EqualFold(field[0], name) {
			values = append(values, field[1])
		}
	}
	return values
}

// Message is an RTSP request or response.
type Message struct {
	// A request's.
	Method, URI string
	// A response's: its status, nonzero, and its words.
	Status int
	Reason string
	Header Header
	Body   []byte
}

// IsRequest says whether m is a request rather than a response.
func (m *Message) IsRequest() bool {
	return m.Status == 0
}

// Read reads the next message.
func Read(r *bufio.Reader) (*Message, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("rtsp: %q is no message's first line", line)
	}
	m := &Message{}
	switch Version {
	case parts[0]:
		if m.Status, err = strconv.Atoi(parts[1]); err != nil || m.Status == 0 {
			return nil, fmt.Errorf("rtsp: %q has no status", line)
		}
		m.Reason = parts[2]
	case parts[2]:
		m.Method, m.URI = parts[0], parts[1]
	default:
		return nil, fmt.Errorf("rtsp: %q is not RTSP 1.0", line)
	}
	for {
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		if line == "" {
			break
		}
		name, value, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("rtsp: %q is no header field", line)
		}
		m.Header = append(m.Header, [2]string{strings.TrimSpace(name), strings.TrimSpace(value)})
	}
	if length := m.Header.Get("Content-Length"); length != "" {
		n, err := strconv.Atoi(length)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("rtsp: Content-Length %q", length)
		}
		m.Body = make([]byte, n)
		if _, err := io.ReadFull(r, m.Body); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// readLine reads a line, its CRLF or LF dropped.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Bytes is m as it goes on the wire.
func (m *Message) Bytes() []byte {
	var b bytes.Buffer
	if m.IsRequest() {
		fmt.Fprintf(&b, "%s %s %s\r\n", m.Method, m.URI, Version)
	} else {
		fmt.Fprintf(&b, "%s %d %s\r\n", Version, m.Status, m.Reason)
	}
	for _, field := range m.Header {
		fmt.Fprintf(&b, "%s: %s\r\n", field[0], field[1])
	}
	if len(m.Body) > 0 {
		fmt.Fprintf(&b, "Content-Length: %d\r\n", len(m.Body))
	}
	b.WriteString("\r\n")
	b.Write(m.Body)
	return b.Bytes()
}

// Interleaved is the marker an interleaved packet starts with, where a message starts
// with a letter.
const Interleaved = '$'

// ReadInterleaved reads an interleaved packet, its marker next: the channel it is on,
// and the packet.
func ReadInterleaved(r *bufio.Reader) (byte, []byte, error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, err
	}
	if head[0] != Interleaved {
		return 0, nil, fmt.Errorf("rtsp: % x is no interleaved packet", head)
	}
	packet := make([]byte, binary.BigEndian.Uint16(head[2:]))
	if _, err := io.ReadFull(r, packet); err != nil {
		return 0, nil, err
	}
	return head[1], packet, nil
}

// AppendInterleaved appends packet, interleaved on channel.
func AppendInterleaved(b []byte, channel byte, packet []byte) []byte {
	b = append(b, Interleaved, channel)
	b = binary.BigEndian.AppendUint16(b, uint16(len(packet)))
	return append(b, packet...)
}
