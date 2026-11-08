// Package rtsptest is an RTSP camera for tests and fake cameras: its streams described,
// set up and played, each in a session of its own, to the client's UDP ports or
// interleaved on the connection, to a user it authenticates with Digest.
package rtsptest

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"greendrake.nz/go/rtsp/internal/wire"
)

// Options configure a camera.
type Options struct {
	// The user it takes, with the password.
	User, Password string
	// Its streams, by path: "/0".
	Streams map[string]Stream
	// The session timeout it hands out, in seconds; 60 if 0.
	Timeout int
	// Set to serve one connection at a time, as some BITVISION cameras do: every other
	// one is accepted and left unanswered until those before it have closed.
	Serial bool
}

// Stream is a stream of one media.
type Stream struct {
	// "video" or "audio", its encoding name and its clock rate, as its SDP has them:
	// "video", "H265", 90000.
	Type, Encoding string
	ClockRate      int
	// The format's parameters, as its SDP's fmtp has them; none if empty.
	Params string
	// Frames a second, as its SDP's framerate has them; none if 0.
	FrameRate float64
	// Set for timestamps that stand still, every packet's 0, as a BITVISION camera's extra
	// stream's do.
	Still bool
	// What a player is sent, over and over, at the pace of their times: H.265 access
	// units. A stream without any is described and set up, and sends nothing.
	Units []Unit
}

// Video is a stream of video in encoding, on RTP's 90 kHz clock, playing units.
func Video(encoding string, units []Unit) Stream {
	return Stream{Type: "video", Encoding: encoding, ClockRate: 90000, Units: units}
}

// Unit is an access unit, its NAL units due at PTS.
type Unit struct {
	NALUs [][]byte
	PTS   time.Duration
}

// realm and nonce are what the camera challenges a request with.
const (
	realm = "rtsptest"
	nonce = "5d41402abc4b2a76"
	// payloadType is the dynamic payload type every stream goes under.
	payloadType = 96
	// maxPayload is the most a packet carries: what fits a LAN's frames.
	maxPayload = 1400
)

// Camera serves until closed.
type Camera struct {
	options  Options
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	// Its connections, and what accepts them.
	serving sync.WaitGroup
	// The one connection served at a time, held by it, where Serial is set.
	serial chan struct{}

	mu sync.Mutex
	// The requests it has received, by method.
	requests map[string]int
	// The packets its UDP ports have received from the client's, by kind: "RTP", "RTCP".
	heard map[string]int
	// How many sessions it has set up: the last one's id.
	sessions int
	// Set once it no longer answers.
	held  bool
	conns map[net.Conn]struct{}
	// Set once closed, after which a connection accepted is dropped.
	closed bool
}

// Listen starts a camera on address, host:port.
func Listen(address string, options Options) (*Camera, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Camera{options: options, listener: listener, ctx: ctx, cancel: cancel, serial: make(chan struct{}, 1), requests: map[string]int{}, heard: map[string]int{}, conns: map[net.Conn]struct{}{}}
	c.serving.Go(c.accept)
	return c, nil
}

// Serve starts a camera for t on a port of its own, closed as t ends.
func Serve(t testing.TB, options Options) *Camera {
	t.Helper()
	c, err := Listen("127.0.0.1:0", options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

// Address is where the camera listens, host:port.
func (c *Camera) Address() string {
	return c.listener.Addr().String()
}

// Requests is how many requests of method the camera has received.
func (c *Camera) Requests(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests[method]
}

// Heard is how many packets the camera's UDP port of kind, "RTP" or "RTCP", has received
// from the client's port of the same kind: what opens the way through a NAT between.
func (c *Camera) Heard(kind string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.heard[kind]
}

// Hold has the camera answer no request from now on, its connections held open: a
// camera gone without closing them.
func (c *Camera) Hold() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.held = true
}

// Close stops the camera listening, drops every connection, and waits for their
// sessions to end.
func (c *Camera) Close() {
	c.cancel()
	c.listener.Close()
	c.mu.Lock()
	c.closed = true
	for conn := range c.conns {
		conn.Close()
	}
	c.mu.Unlock()
	c.serving.Wait()
}

func (c *Camera) accept() {
	for {
		conn, err := c.listener.Accept()
		if err != nil {
			return
		}
		// A connection the listener took before it was closed, but handed over after.
		c.mu.Lock()
		closed := c.closed
		if !closed {
			c.conns[conn] = struct{}{}
		}
		c.mu.Unlock()
		if closed {
			conn.Close()
			continue
		}
		c.serving.Go(func() {
			(&connection{camera: c, conn: conn, sessions: map[string]*session{}}).serve()
		})
	}
}

// connection is a client's connection: the sessions set up on it.
type connection struct {
	camera   *Camera
	conn     net.Conn
	writes   sync.Mutex
	sessions map[string]*session
}

// session is a stream set up on a connection, and played.
type session struct {
	conn   *connection
	stream Stream
	// Where RTP goes: over UDP, from the RTP port to the client's; or interleaved on the
	// channel.
	rtp, rtcp *net.UDPConn
	client    *net.UDPAddr
	channel   byte
	// Ends the stream played, once it is.
	stop context.CancelFunc
}

// serve answers the connection's requests until it closes, which ends its sessions: once
// it holds the one connection served, where the camera is Serial.
func (cn *connection) serve() {
	c := cn.camera
	if c.options.Serial {
		select {
		case c.serial <- struct{}{}:
			// Given up once the connection is closed, which the deferral below does first.
			defer func() { <-c.serial }()
		case <-c.ctx.Done():
			cn.close()
			return
		}
	}
	defer cn.close()
	r := bufio.NewReader(cn.conn)
	for {
		req, err := wire.Read(r)
		if err != nil {
			return
		}
		c.mu.Lock()
		c.requests[req.Method]++
		held := c.held
		c.mu.Unlock()
		if held {
			continue
		}
		res, played := cn.answer(req)
		res.Header = append(wire.Header{{"CSeq", req.Header.Get("CSeq")}}, res.Header...)
		if err := cn.write(res.Bytes()); err != nil {
			return
		}
		if played != nil {
			ctx, stop := context.WithCancel(c.ctx)
			played.stop = stop
			go played.play(ctx)
		}
	}
}

func (cn *connection) write(b []byte) error {
	cn.writes.Lock()
	defer cn.writes.Unlock()
	_, err := cn.conn.Write(b)
	return err
}

// authorised says whether req carries the camera's user's Digest response.
func (cn *connection) authorised(req *wire.Message) bool {
	scheme, rest, _ := strings.Cut(req.Header.Get("Authorization"), " ")
	if scheme != "Digest" {
		return false
	}
	fields := wire.Params(rest)
	o := cn.camera.options
	return fields["username"] == o.User && fields["response"] == wire.Digest(o.User, realm, o.Password, req.Method, fields["uri"], nonce, "", "", "")
}

// streamOf is the stream a request's URL names, by its path: a setup's names its media
// under it.
func (cn *connection) streamOf(uri string) (Stream, bool) {
	u, err := url.Parse(uri)
	if err != nil {
		return Stream{}, false
	}
	path, _, _ := strings.Cut(u.Path, "/trackID=")
	stream, served := cn.camera.options.Streams[strings.TrimSuffix(path, "/")]
	return stream, served
}

func response(status int, reason string, header wire.Header) *wire.Message {
	return &wire.Message{Status: status, Reason: reason, Header: header}
}

// answer answers req, and names the session it has the camera start playing, if any.
func (cn *connection) answer(req *wire.Message) (*wire.Message, *session) {
	if req.Method == "OPTIONS" {
		return response(200, "OK", wire.Header{{"Public", "OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN"}}), nil
	}
	if !cn.authorised(req) {
		return response(401, "Unauthorized", wire.Header{{"WWW-Authenticate", fmt.Sprintf(`Digest realm="%s", nonce="%s"`, realm, nonce)}}), nil
	}
	switch req.Method {
	case "DESCRIBE":
		stream, served := cn.streamOf(req.URI)
		if !served {
			return response(404, "Not Found", nil), nil
		}
		sdp := fmt.Sprintf("v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=rtsptest\r\nt=0 0\r\nm=%s 0 RTP/AVP %d\r\na=rtpmap:%d %s/%d\r\n",
			stream.Type, payloadType, payloadType, stream.Encoding, stream.ClockRate)
		if stream.Params != "" {
			sdp += fmt.Sprintf("a=fmtp:%d %s\r\n", payloadType, stream.Params)
		}
		if stream.FrameRate != 0 {
			sdp += fmt.Sprintf("a=framerate:%g\r\n", stream.FrameRate)
		}
		res := response(200, "OK", wire.Header{{"Content-Base", strings.TrimSuffix(req.URI, "/") + "/"}, {"Content-Type", "application/sdp"}})
		res.Body = []byte(sdp + "a=control:trackID=0\r\n")
		return res, nil
	case "SETUP":
		// Each setup a session of its own, alongside any other on the connection.
		stream, served := cn.streamOf(req.URI)
		if !served {
			return response(404, "Not Found", nil), nil
		}
		s := &session{conn: cn, stream: stream}
		answer, err := s.setUp(req.Header.Get("Transport"))
		if err != nil {
			return response(461, "Unsupported Transport", nil), nil
		}
		c := cn.camera
		c.mu.Lock()
		c.sessions++
		id := fmt.Sprintf("rtsptest%d", c.sessions)
		c.mu.Unlock()
		cn.sessions[id] = s
		timeout := c.options.Timeout
		if timeout == 0 {
			timeout = 60
		}
		return response(200, "OK", wire.Header{{"Transport", answer}, {"Session", fmt.Sprintf("%s;timeout=%d", id, timeout)}}), nil
	case "PLAY", "TEARDOWN":
		id := req.Header.Get("Session")
		s, set := cn.sessions[id]
		if !set {
			return response(454, "Session Not Found", nil), nil
		}
		if req.Method == "PLAY" {
			return response(200, "OK", nil), s
		}
		s.end()
		delete(cn.sessions, id)
		return response(200, "OK", nil), nil
	}
	return response(501, "Not Implemented", nil), nil
}

// close ends the connection's sessions and closes it.
func (cn *connection) close() {
	for _, s := range cn.sessions {
		s.end()
	}
	cn.conn.Close()
	c := cn.camera
	c.mu.Lock()
	delete(c.conns, cn.conn)
	c.mu.Unlock()
}

// setUp sets the session's transport up as the client asks it, and answers what it set up:
// over UDP, from a pair of ports of its own to the client's, or interleaved over TCP.
func (s *session) setUp(transport string) (string, error) {
	if strings.HasPrefix(transport, "RTP/AVP/TCP") {
		channel, err := wire.InterleavedChannel(transport)
		s.channel = channel
		return transport, err
	}
	rtpPort, rtcpPort, err := wire.Pair(transport, "client_port")
	if err != nil {
		return "", err
	}
	conn := s.conn.conn
	if s.rtp, s.rtcp, err = wire.ListenPair(conn.LocalAddr().(*net.TCPAddr).IP); err != nil {
		return "", err
	}
	client := conn.RemoteAddr().(*net.TCPAddr).IP
	s.client = &net.UDPAddr{IP: client, Port: rtpPort}
	go s.hear(s.rtp, s.client, "RTP")
	go s.hear(s.rtcp, &net.UDPAddr{IP: client, Port: rtcpPort}, "RTCP")
	own := s.rtp.LocalAddr().(*net.UDPAddr).Port
	return fmt.Sprintf("%s;server_port=%d-%d", transport, own, own+1), nil
}

// end stops the session playing, if it is, and closes its ports.
func (s *session) end() {
	if s.stop != nil {
		s.stop()
	}
	if s.rtp != nil {
		s.rtp.Close()
		s.rtcp.Close()
	}
}

// hear counts the packets port receives from the client's port of kind, from, until it
// closes.
func (s *session) hear(port *net.UDPConn, from *net.UDPAddr, kind string) {
	c := s.conn.camera
	wire.Receive(port, func(_ []byte, sender *net.UDPAddr) bool {
		if sender.IP.Equal(from.IP) && sender.Port == from.Port {
			c.mu.Lock()
			c.heard[kind]++
			c.mu.Unlock()
		}
		return true
	})
}

// send sends a unit's packets as the session is set up to.
func (s *session) send(packets [][]byte) error {
	if s.rtp != nil {
		for _, packet := range packets {
			if _, err := s.rtp.WriteToUDP(packet, s.client); err != nil {
				return err
			}
		}
		return nil
	}
	var b []byte
	for _, packet := range packets {
		b = wire.AppendInterleaved(b, s.channel, packet)
	}
	return s.conn.write(b)
}

// play sends the stream's units over and over, each at its time, as RTP packets as the
// session is set up, until ctx ends. The units come round again a unit's average span
// after the last.
func (s *session) play(ctx context.Context) {
	units := s.stream.Units
	if len(units) == 0 {
		return
	}
	span := units[len(units)-1].PTS - units[0].PTS
	round := span + span/time.Duration(max(len(units)-1, 1))
	var sequence uint16
	started := time.Now()
	for offset := time.Duration(0); ; offset += round {
		for _, unit := range units {
			due := offset + unit.PTS - units[0].PTS
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(started.Add(due))):
			}
			var timestamp uint32
			if !s.stream.Still {
				timestamp = uint32(int64(due) * int64(s.stream.ClockRate) / int64(time.Second))
			}
			var packets [][]byte
			for i, nalu := range unit.NALUs {
				payloads := Payloads(nalu, maxPayload)
				for j, payload := range payloads {
					last := i == len(unit.NALUs)-1 && j == len(payloads)-1
					packets = append(packets, wire.AppendPacket(nil, wire.Packet{PayloadType: payloadType, Marker: last, Sequence: sequence, Timestamp: timestamp, Payload: payload}, 1))
					sequence++
				}
			}
			if err := s.send(packets); err != nil {
				return
			}
		}
	}
}

// Payloads are the RTP payloads of an H.265 NAL unit, of size bytes at most: itself where
// it fits, and fragmentation units otherwise (RFC 7798 4.4.3).
func Payloads(nalu []byte, size int) [][]byte {
	if len(nalu) <= size {
		return [][]byte{nalu}
	}
	kind := wire.NALType(nalu)
	// The payload header: the NAL unit's, with the fragmentation unit's type.
	header := []byte{nalu[0]&0x81 | 49<<1, nalu[1]}
	var payloads [][]byte
	for rest := nalu[2:]; len(rest) > 0; {
		n := min(len(rest), size-3)
		fu := kind
		if len(payloads) == 0 {
			fu |= 0x80
		}
		if n == len(rest) {
			fu |= 0x40
		}
		payloads = append(payloads, append(append(append([]byte{}, header...), fu), rest[:n]...))
		rest = rest[n:]
	}
	return payloads
}
