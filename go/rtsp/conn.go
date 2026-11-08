// Package rtsp is an RTSP 1.0 client (RFC 2326), as thin as playing an IP camera's
// streams takes: requests on one TCP connection, authenticated as the camera asks (Basic,
// or Digest), each stream played as a session of its own on it, its RTP packets sent to a
// pair of the client's UDP ports or interleaved on the connection, and H.265 put back
// together from them.
package rtsp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"greendrake.nz/go/rtsp/internal/wire"
)

// Packet is an RTP packet of a stream played.
type Packet = wire.Packet

const (
	// answerTimeout bounds a request, and a write: a server that leaves one unanswered
	// that long is gone.
	answerTimeout = 3 * time.Second
	// defaultSessionTimeout is how long a server keeps a session it names no timeout of
	// without hearing from the client (RFC 2326 12.37).
	defaultSessionTimeout = 60 * time.Second
	// defaultPort is RTSP's, where a server listens if its URL names no port (RFC 2326 3.2).
	defaultPort = "554"
	userAgent   = "greendrake-rtsp"
	// How many of a stream's packets a session holds for its reader.
	packetBuffer = 256
	// How much a session's RTP port holds of what arrives before it is read: a key frame's
	// burst, several times over, as far as the system lets a socket have
	// (net.core.rmem_max).
	readBuffer = 4 << 20
	// The most interleaved channels a connection has: RTP's and RTCP's of each session.
	interleavedChannels = 256
)

var (
	// ErrNoAnswer is a request the server left unanswered, which ends the connection.
	ErrNoAnswer = errors.New("rtsp: the server left a request unanswered")
	// ErrClosed is why a connection, or a session, Closed has ended.
	ErrClosed = errors.New("rtsp: closed")
)

// Transport is how the packets of a stream played come.
type Transport int

const (
	// UDP, the zero Transport: to a pair of the client's UDP ports, RTP's and RTCP's, as
	// cameras send them. The client sends from them first, so that a NAT between lets the
	// server's in, which it does as long as it keeps their numbers.
	UDP Transport = iota
	// TCP: interleaved on the connection, for a camera that sends them so.
	TCP
)

// StatusError is the server's refusal of a request.
type StatusError struct {
	Method string
	Status int
	Reason string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("rtsp: %s refused: %d %s", e.Method, e.Status, e.Reason)
}

// Conn is a connection to an RTSP server, as a user: its requests go out one at a time,
// and the streams played on it, each a Session, play on it at once.
type Conn struct {
	nc net.Conn
	// The host:port of the server, which every URL names.
	host string
	// The server's address, which its packets over UDP come from, and the client's go to.
	server net.IP
	// Serialises the requests answers are waited for.
	requests sync.Mutex
	// Holds a message, or a packet, whole on its way out.
	writes sync.Mutex

	mu   sync.Mutex
	cseq int
	auth authorizer
	// The request an answer is waited for, by its CSeq, and where its answer goes.
	waiting int
	answer  chan *wire.Message
	// The sessions on the connection, from their setting up until they end.
	sessions map[*Session]struct{}

	closing   chan struct{}
	closeOnce sync.Once
	// Closed as the connection ends, under mu, before its sessions end and before Done.
	ended chan struct{}
	done  chan struct{}
	err   error
}

// Session is a stream played on a connection: one media set up and played, its RTP
// packets arriving on Packets, kept alive for as long as it lasts.
type Session struct {
	conn    *Conn
	packets chan Packet
	// Set under the connection's mu: the server's id of the session and where it plays,
	// once it is set up; whether its packets come interleaved, and on which channel.
	id, control string
	interleaved bool
	channel     byte
	// Over UDP, the ports its packets arrive on, and their reader. RTCP's goes unread.
	rtp, rtcp *net.UDPConn
	receiving sync.WaitGroup
	// Closed as the session ends, under the connection's mu, before its packets close.
	ended   chan struct{}
	endOnce sync.Once
	err     error
}

// Dial connects to the server at address, host[:port], at RTSP's port if it names none,
// as user with password, which are sent only once the server asks for them. ctx bounds
// the connecting.
func Dial(ctx context.Context, address, user, password string) (*Conn, error) {
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, defaultPort)
	}
	var dialer net.Dialer
	nc, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	c := &Conn{
		nc:       nc,
		host:     address,
		server:   nc.RemoteAddr().(*net.TCPAddr).IP,
		auth:     authorizer{user: user, password: password},
		answer:   make(chan *wire.Message, 1),
		sessions: map[*Session]struct{}{},
		closing:  make(chan struct{}),
		ended:    make(chan struct{}),
		done:     make(chan struct{}),
	}
	go c.read()
	return c, nil
}

// Done is closed once the connection has ended, with Err saying why, and every session on
// it with it.
func (c *Conn) Done() <-chan struct{} {
	return c.done
}

func (c *Conn) Err() error {
	return c.err
}

// Close ends the connection, tearing down every session on it, without waiting for the
// server to say it has.
func (c *Conn) Close() {
	c.closeOnce.Do(func() {
		close(c.closing)
		c.mu.Lock()
		var set []*Session
		for s := range c.sessions {
			if s.id != "" {
				set = append(set, s)
			}
		}
		c.mu.Unlock()
		for _, s := range set {
			// The socket closes next whatever the write does.
			c.post("TEARDOWN", s.control, s.id)
		}
		c.nc.Close()
	})
	<-c.done
}

// url is where path is on the server.
func (c *Conn) url(path string) string {
	return "rtsp://" + c.host + path
}

// Options asks the server what it supports of the stream at path: a probe of the
// connection.
func (c *Conn) Options(ctx context.Context, path string) error {
	_, err := c.request(ctx, "OPTIONS", c.url(path), "", nil)
	return err
}

// Describe asks for the description of the stream at path.
func (c *Conn) Describe(ctx context.Context, path string) (*Description, error) {
	uri := c.url(path)
	res, err := c.request(ctx, "DESCRIBE", uri, "", wire.Header{{"Accept", "application/sdp"}})
	if err != nil {
		return nil, err
	}
	base := uri
	for _, field := range []string{"Content-Base", "Content-Location"} {
		if value := res.Header.Get(field); value != "" {
			base = value
			break
		}
	}
	return parseDescription(res.Body, base)
}

// Play sets media up as a session of its own, its packets coming by transport, and plays
// the stream d describes in it: media's RTP packets arrive on the session's Packets. The
// session is kept alive for as long as it lasts, alongside any other on the connection.
// The setup is waited out whatever ctx does, up to the answer timeout, since its answer
// alone names the session: one set up as ctx ended is torn down, not left to the server.
func (c *Conn) Play(ctx context.Context, d *Description, media Media, transport Transport) (*Session, error) {
	s := &Session{conn: c, packets: make(chan Packet, packetBuffer), ended: make(chan struct{})}
	request, err := c.open(s, transport)
	if err != nil {
		return nil, err
	}
	if err := s.play(ctx, d, media, request); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// open readies s on the connection, unless it has ended, to receive by transport: over
// UDP a pair of ports and their reader, over TCP the lowest pair of interleaved channels
// no other session has. It answers the Transport field the setup asks for.
func (c *Conn) open(s *Session, transport Transport) (string, error) {
	var rtp, rtcp *net.UDPConn
	if transport == UDP {
		var err error
		if rtp, rtcp, err = wire.ListenPair(nil); err != nil {
			return "", err
		}
		if err := rtp.SetReadBuffer(readBuffer); err != nil {
			rtp.Close()
			rtcp.Close()
			return "", err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.ended:
		if rtp != nil {
			rtp.Close()
			rtcp.Close()
		}
		return "", c.err
	default:
	}
	if rtp == nil {
		taken := map[int]bool{}
		for other := range c.sessions {
			if other.interleaved {
				taken[int(other.channel)] = true
			}
		}
		channel := 0
		for taken[channel] {
			channel += 2
		}
		if channel >= interleavedChannels {
			return "", errors.New("rtsp: no interleaved channels free")
		}
		s.interleaved, s.channel = true, byte(channel)
		c.sessions[s] = struct{}{}
		return fmt.Sprintf("RTP/AVP/TCP;unicast;interleaved=%d-%d", channel, channel+1), nil
	}
	s.rtp, s.rtcp = rtp, rtcp
	c.sessions[s] = struct{}{}
	s.receiving.Go(s.receive)
	port := rtp.LocalAddr().(*net.UDPAddr).Port
	return fmt.Sprintf("RTP/AVP;unicast;client_port=%d-%d", port, port+1), nil
}

// play sets the session up with the Transport field transport, and plays it.
func (s *Session) play(ctx context.Context, d *Description, media Media, transport string) error {
	c := s.conn
	// The setup's answer is waited for whatever ctx does: it alone names the session, which
	// the server keeps until it is torn down.
	res, err := c.request(context.WithoutCancel(ctx), "SETUP", media.Control, "", wire.Header{{"Transport", transport}})
	if err != nil {
		return err
	}
	id, timeout, err := parseSession(res.Header.Get("Session"))
	if err != nil {
		return err
	}
	// Set up, the session is the server's to tear down, whatever fails from here on.
	c.mu.Lock()
	s.id, s.control = id, d.Control
	c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	answer := res.Header.Get("Transport")
	if s.interleaved {
		channel, err := wire.InterleavedChannel(answer)
		if err != nil {
			return err
		}
		// A session put on another's channel keeps the free one it asked for until it is
		// torn down, so that the other's packets stay the other's meanwhile.
		c.mu.Lock()
		taken := slices.ContainsFunc(slices.Collect(maps.Keys(c.sessions)), func(other *Session) bool {
			return other != s && other.interleaved && other.channel == channel
		})
		if !taken {
			s.channel = channel
		}
		c.mu.Unlock()
		if taken {
			return fmt.Errorf("rtsp: the server put the session on interleaved channel %d, another session's", channel)
		}
	} else if err := s.punch(answer); err != nil {
		return err
	}
	if _, err := c.request(ctx, "PLAY", d.Control, id, wire.Header{{"Range", "npt=0.000-"}}); err != nil {
		return err
	}
	go s.keepAlive(timeout / 2)
	return nil
}

// Packets are the stream's, as they arrive: closed once the session has ended, with Err
// saying why. A session whose reader falls a buffer behind loses what arrives meanwhile:
// over UDP once its port's buffer is full too, and interleaved at once, rather than hold
// up the connection's other sessions and its answers.
func (s *Session) Packets() <-chan Packet {
	return s.packets
}

// Err is why the session ended, once its Packets have closed: ErrClosed if it was
// closed, or else why its connection ended.
func (s *Session) Err() error {
	return s.err
}

// Close tears the session down, leaving the connection to the others, without waiting for
// the server to say it has.
func (s *Session) Close() {
	c := s.conn
	c.mu.Lock()
	_, live := c.sessions[s]
	set := live && s.id != ""
	c.mu.Unlock()
	if set {
		if err := c.post("TEARDOWN", s.control, s.id); err != nil {
			c.Close()
		}
	}
	s.end(ErrClosed)
}

// end ends the session for err, once: off the connection, its ports closed and their
// reader gone, then its packets closed.
func (s *Session) end(err error) {
	s.endOnce.Do(func() {
		c := s.conn
		c.mu.Lock()
		delete(c.sessions, s)
		s.err = err
		close(s.ended)
		c.mu.Unlock()
		if s.rtp != nil {
			s.rtp.Close()
			s.rtcp.Close()
		}
		s.receiving.Wait()
		close(s.packets)
	})
}

// keepAlive asks the server what it supports of the session every period, which keeps it
// alive, and ends the connection with a request it cannot write. The answers go unheeded:
// a server that has stopped answering stops streaming too.
func (s *Session) keepAlive(period time.Duration) {
	c := s.conn
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-s.ended:
			return
		case <-ticker.C:
			if err := c.post("OPTIONS", s.control, s.id); err != nil {
				c.Close()
				return
			}
		}
	}
}

// receive passes on the RTP packets the server sends to the session's RTP port until it
// closes.
func (s *Session) receive() {
	server := s.conn.server
	wire.Receive(s.rtp, func(datagram []byte, from *net.UDPAddr) bool {
		p, err := wire.ParsePacket(datagram)
		if err != nil || !from.IP.Equal(server) {
			return true
		}
		select {
		case s.packets <- p:
			return true
		case <-s.ended:
			return false
		}
	})
}

// punch sends a packet from each of the session's UDP ports to the server's that
// transport, the setup's answer, names: from RTP's an RTP packet with no payload, and from
// RTCP's a receiver report of none (RFC 3550 6.4.2). A NAT between lets in only what
// answers packets sent out, so this lets the server's in.
func (s *Session) punch(transport string) error {
	rtpPort, rtcpPort, err := wire.Pair(transport, "server_port")
	if err != nil {
		return err
	}
	server := s.conn.server
	if _, err := s.rtp.WriteToUDP(wire.AppendPacket(nil, wire.Packet{}, 0), &net.UDPAddr{IP: server, Port: rtpPort}); err != nil {
		return err
	}
	// Version 2 and no reports, type 201, a length of one word past the first, and an SSRC
	// of 0.
	_, err = s.rtcp.WriteToUDP([]byte{0x80, 201, 0, 1, 0, 0, 0, 0}, &net.UDPAddr{IP: server, Port: rtcpPort})
	return err
}

// parseSession reads a Session field, "<id>[;timeout=<seconds>]".
func parseSession(field string) (string, time.Duration, error) {
	id, params, _ := strings.Cut(field, ";")
	if id = strings.TrimSpace(id); id == "" {
		return "", 0, fmt.Errorf("rtsp: no session in %q", field)
	}
	timeout := defaultSessionTimeout
	for _, param := range strings.Split(params, ";") {
		if value, found := strings.CutPrefix(strings.TrimSpace(param), "timeout="); found {
			seconds, err := strconv.Atoi(value)
			if err != nil || seconds <= 0 {
				return "", 0, fmt.Errorf("rtsp: session timeout %q", field)
			}
			timeout = time.Duration(seconds) * time.Second
		}
	}
	return id, timeout, nil
}

// request makes a request of method on uri, in the session named, if one is, and
// authenticating once as the server asks, and answers the server's response, a success.
// The server leaving it unanswered ends the connection; ctx ending abandons it alone,
// its answer dropped if one comes.
func (c *Conn) request(ctx context.Context, method, uri, session string, header wire.Header) (*wire.Message, error) {
	c.requests.Lock()
	defer c.requests.Unlock()
	for asked := false; ; asked = true {
		res, err := c.exchange(ctx, c.requestOf(method, uri, session, header))
		if err != nil {
			return nil, err
		}
		if res.Status == 401 && !asked {
			if challenge, err := challengeOf(res.Header.Values("WWW-Authenticate")); err == nil {
				c.mu.Lock()
				c.auth.challenge, c.auth.count = challenge, 0
				c.mu.Unlock()
				continue
			}
		}
		if res.Status < 200 || res.Status > 299 {
			return nil, &StatusError{Method: method, Status: res.Status, Reason: res.Reason}
		}
		return res, nil
	}
}

// post makes a request of method on uri, in the session named, whose answer is not waited
// for: it is dropped when it comes.
func (c *Conn) post(method, uri, session string) error {
	return c.send(c.requestOf(method, uri, session, nil))
}

// requestOf is a request of method on uri, in the session named, if one is, with header
// and what every request carries: its sequence number and the authorization the server
// asked for.
func (c *Conn) requestOf(method, uri, session string, header wire.Header) *wire.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cseq++
	fields := wire.Header{{"CSeq", strconv.Itoa(c.cseq)}, {"User-Agent", userAgent}}
	if session != "" {
		fields = append(fields, [2]string{"Session", session})
	}
	if c.auth.challenge != nil {
		fields = append(fields, [2]string{"Authorization", c.auth.authorization(method, uri)})
	}
	return &wire.Message{Method: method, URI: uri, Header: append(fields, header...)}
}

// exchange sends req and waits for its response.
func (c *Conn) exchange(ctx context.Context, req *wire.Message) (*wire.Message, error) {
	cseq, _ := strconv.Atoi(req.Header.Get("CSeq"))
	c.mu.Lock()
	c.waiting = cseq
	c.mu.Unlock()
	if err := c.send(req); err != nil {
		c.Close()
		return nil, err
	}
	timer := time.NewTimer(answerTimeout)
	defer timer.Stop()
	select {
	case res := <-c.answer:
		return res, nil
	case <-c.done:
		return nil, c.err
	case <-timer.C:
		c.Close()
		return nil, ErrNoAnswer
	case <-ctx.Done():
		// The answer is no longer waited for; one that has come already goes too.
		c.mu.Lock()
		c.waiting = 0
		select {
		case <-c.answer:
		default:
		}
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// send writes m whole.
func (c *Conn) send(m *wire.Message) error {
	c.writes.Lock()
	defer c.writes.Unlock()
	c.nc.SetWriteDeadline(time.Now().Add(answerTimeout))
	_, err := c.nc.Write(m.Bytes())
	return err
}

// read reads the connection until it ends: the packets played, the responses waited for,
// and the server's own requests, answered.
func (c *Conn) read() {
	r := bufio.NewReader(c.nc)
	err := func() error {
		for {
			next, err := r.Peek(1)
			if err != nil {
				return err
			}
			if next[0] == wire.Interleaved {
				if err := c.deliver(r); err != nil {
					return err
				}
				continue
			}
			m, err := wire.Read(r)
			if err != nil {
				return err
			}
			if m.IsRequest() {
				// A server asks after the client with OPTIONS; it gets nothing else answered.
				status, reason := 501, "Not Implemented"
				if m.Method == "OPTIONS" {
					status, reason = 200, "OK"
				}
				if err := c.send(&wire.Message{Status: status, Reason: reason, Header: wire.Header{{"CSeq", m.Header.Get("CSeq")}}}); err != nil {
					return err
				}
				continue
			}
			// The answer waited for goes to its waiter, once; any other, a keep-alive's or
			// an abandoned request's, is dropped.
			cseq, _ := strconv.Atoi(m.Header.Get("CSeq"))
			c.mu.Lock()
			if cseq != 0 && cseq == c.waiting {
				c.waiting = 0
				c.answer <- m
			}
			c.mu.Unlock()
		}
	}()
	select {
	case <-c.closing:
		// Close closes the socket, once its teardowns have gone out on it.
		err = ErrClosed
	default:
		// Ended by the server, or broken, the connection is closed this end too.
		c.nc.Close()
	}
	c.mu.Lock()
	c.err = err
	close(c.ended)
	sessions := slices.Collect(maps.Keys(c.sessions))
	c.mu.Unlock()
	for _, s := range sessions {
		s.end(err)
	}
	close(c.done)
}

// deliver reads an interleaved packet, and passes it on to the session it is an RTP
// packet of: RTCP, and a packet that is no RTP packet, are dropped.
func (c *Conn) deliver(r *bufio.Reader) error {
	channel, data, err := wire.ReadInterleaved(r)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for s := range c.sessions {
		if !s.interleaved || s.channel != channel {
			continue
		}
		p, err := wire.ParsePacket(data)
		if err != nil {
			return nil
		}
		select {
		case s.packets <- p:
		default:
		}
		return nil
	}
	return nil
}
