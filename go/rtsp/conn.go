// Package rtsp is an RTSP 1.0 client (RFC 2326), as thin as playing an IP camera's
// stream takes: requests on one TCP connection, authenticated as the camera asks (Basic,
// or Digest), the stream's RTP packets sent to a pair of the client's UDP ports or
// interleaved on the connection, and H.265 put back together from them.
package rtsp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"greendrake.nz/go/rtsp/internal/wire"
)

// Packet is an RTP packet of the stream played.
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
	// How many of the stream's packets the connection holds for its reader.
	packetBuffer = 256
	// How much the RTP port holds of what arrives before it is read: a key frame's burst,
	// several times over, as far as the system lets a socket have (net.core.rmem_max).
	readBuffer = 4 << 20
)

var (
	// ErrNoAnswer is a request the server left unanswered, which ends the connection.
	ErrNoAnswer = errors.New("rtsp: the server left a request unanswered")
	// ErrClosed is why a connection Closed has ended.
	ErrClosed = errors.New("rtsp: the connection was closed")
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
// and a stream played on it arrives on it too.
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
	// The session set up, and where it is played; empty until it is.
	session, playing string
	// The request an answer is waited for, by its CSeq.
	waiting int
	answer  chan *wire.Message

	// The ports the packets played arrive on over UDP, RTP's and RTCP's, set under mu.
	rtp, rtcp *net.UDPConn
	// The goroutine reading the RTP port.
	receiving sync.WaitGroup

	// The interleaved channel the packets played arrive on over TCP, plus one; none until
	// set up.
	channel atomic.Uint32
	packets chan Packet

	closing   chan struct{}
	closeOnce sync.Once
	// Closed as the connection ends, before Done.
	ended chan struct{}
	done  chan struct{}
	err   error
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
		nc:      nc,
		host:    address,
		server:  nc.RemoteAddr().(*net.TCPAddr).IP,
		auth:    authorizer{user: user, password: password},
		answer:  make(chan *wire.Message, 1),
		packets: make(chan Packet, packetBuffer),
		closing: make(chan struct{}),
		ended:   make(chan struct{}),
		done:    make(chan struct{}),
	}
	go c.read()
	return c, nil
}

// Done is closed once the connection has ended, with Err saying why.
func (c *Conn) Done() <-chan struct{} {
	return c.done
}

func (c *Conn) Err() error {
	return c.err
}

// Packets are the stream's played, as they arrive: closed once the connection has ended.
func (c *Conn) Packets() <-chan Packet {
	return c.packets
}

// Close ends the connection, tearing down the stream played on it, without waiting for
// the server to say it has.
func (c *Conn) Close() {
	c.closeOnce.Do(func() {
		close(c.closing)
		c.mu.Lock()
		playing := c.playing
		c.mu.Unlock()
		if playing != "" {
			c.send(c.requestOf("TEARDOWN", playing, nil))
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
	_, err := c.request(ctx, "OPTIONS", c.url(path), nil)
	return err
}

// Describe asks for the description of the stream at path.
func (c *Conn) Describe(ctx context.Context, path string) (*Description, error) {
	uri := c.url(path)
	res, err := c.request(ctx, "DESCRIBE", uri, wire.Header{{"Accept", "application/sdp"}})
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

// Play sets media up, its packets coming by transport, and plays the stream d describes:
// media's RTP packets arrive on Packets. The session is kept alive for as long as the
// connection lasts.
func (c *Conn) Play(ctx context.Context, d *Description, media Media, transport Transport) error {
	request := "RTP/AVP/TCP;unicast;interleaved=0-1"
	if transport == UDP {
		rtp, rtcp, err := wire.ListenPair(nil)
		if err == nil {
			err = rtp.SetReadBuffer(readBuffer)
		}
		if err != nil {
			return err
		}
		if err := c.receive(rtp, rtcp); err != nil {
			return err
		}
		port := rtp.LocalAddr().(*net.UDPAddr).Port
		request = fmt.Sprintf("RTP/AVP;unicast;client_port=%d-%d", port, port+1)
	}
	res, err := c.request(ctx, "SETUP", media.Control, wire.Header{{"Transport", request}})
	if err != nil {
		return err
	}
	session, timeout, err := parseSession(res.Header.Get("Session"))
	if err != nil {
		return err
	}
	answer := res.Header.Get("Transport")
	if transport == UDP {
		if err := c.punch(answer); err != nil {
			return err
		}
	} else {
		channel, err := wire.InterleavedChannel(answer)
		if err != nil {
			return err
		}
		c.channel.Store(uint32(channel) + 1)
	}
	c.mu.Lock()
	c.session = session
	c.mu.Unlock()
	if _, err := c.request(ctx, "PLAY", d.Control, wire.Header{{"Range", "npt=0.000-"}}); err != nil {
		return err
	}
	c.mu.Lock()
	c.playing = d.Control
	c.mu.Unlock()
	go c.keepAlive(d.Control, timeout/2)
	return nil
}

// keepAlive asks the server what it supports every period, which keeps the session
// alive, and ends the connection with a request it cannot write. The answers go
// unheeded: a server that has stopped answering stops streaming too.
func (c *Conn) keepAlive(uri string, period time.Duration) {
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if err := c.send(c.requestOf("OPTIONS", uri, nil)); err != nil {
				c.Close()
				return
			}
		}
	}
}

// receive passes on the RTP packets the server sends to rtp, until the connection ends,
// which closes both ports. RTCP's goes unread.
func (c *Conn) receive(rtp, rtcp *net.UDPConn) error {
	c.mu.Lock()
	select {
	case <-c.ended:
		c.mu.Unlock()
		rtp.Close()
		rtcp.Close()
		return c.err
	default:
	}
	c.rtp, c.rtcp = rtp, rtcp
	c.receiving.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.receiving.Done()
		wire.Receive(rtp, func(datagram []byte, from *net.UDPAddr) bool {
			p, err := wire.ParsePacket(datagram)
			if err != nil || !from.IP.Equal(c.server) {
				return true
			}
			select {
			case c.packets <- p:
				return true
			case <-c.ended:
				return false
			}
		})
	}()
	return nil
}

// punch sends a packet from each of the client's UDP ports to the server's that transport,
// the setup's answer, names: from RTP's an RTP packet with no payload, and from RTCP's a
// receiver report of none (RFC 3550 6.4.2). A NAT between lets in only what answers
// packets sent out, so this lets the server's in.
func (c *Conn) punch(transport string) error {
	rtpPort, rtcpPort, err := wire.Pair(transport, "server_port")
	if err != nil {
		return err
	}
	c.mu.Lock()
	rtp, rtcp := c.rtp, c.rtcp
	c.mu.Unlock()
	if _, err := rtp.WriteToUDP(wire.AppendPacket(nil, wire.Packet{}, 0), &net.UDPAddr{IP: c.server, Port: rtpPort}); err != nil {
		return err
	}
	// Version 2 and no reports, type 201, a length of one word past the first, and an SSRC
	// of 0.
	_, err = rtcp.WriteToUDP([]byte{0x80, 201, 0, 1, 0, 0, 0, 0}, &net.UDPAddr{IP: c.server, Port: rtcpPort})
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

// request makes a request of method on uri, authenticating once as the server asks, and
// answers the server's response, a success. ctx ending abandons the request and the
// connection with it, as does the server leaving it unanswered.
func (c *Conn) request(ctx context.Context, method, uri string, header wire.Header) (*wire.Message, error) {
	c.requests.Lock()
	defer c.requests.Unlock()
	for asked := false; ; asked = true {
		res, err := c.exchange(ctx, c.requestOf(method, uri, header))
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

// requestOf is a request of method on uri, with header and what every request carries:
// its sequence number, the session, and the authorization the server asked for.
func (c *Conn) requestOf(method, uri string, header wire.Header) *wire.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cseq++
	fields := wire.Header{{"CSeq", strconv.Itoa(c.cseq)}, {"User-Agent", userAgent}}
	if c.session != "" {
		fields = append(fields, [2]string{"Session", c.session})
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
		c.Close()
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
			// The answer waited for goes to its waiter, once; any other, a keep-alive's, is
			// dropped.
			cseq, _ := strconv.Atoi(m.Header.Get("CSeq"))
			c.mu.Lock()
			waited := cseq != 0 && cseq == c.waiting
			if waited {
				c.waiting = 0
			}
			c.mu.Unlock()
			if waited {
				c.answer <- m
			}
		}
	}()
	select {
	case <-c.closing:
		// Close closes the socket, once its teardown has gone out on it.
		err = ErrClosed
	default:
		// Ended by the server, or broken, the connection is closed this end too.
		c.nc.Close()
	}
	c.err = err
	close(c.ended)
	// The UDP ports go with the connection, and their reader with them, before the
	// packets close.
	c.mu.Lock()
	rtp, rtcp := c.rtp, c.rtcp
	c.mu.Unlock()
	if rtp != nil {
		rtp.Close()
		rtcp.Close()
	}
	c.receiving.Wait()
	close(c.packets)
	close(c.done)
}

// deliver reads an interleaved packet, and passes it on if it is an RTP packet of the
// stream played: RTCP, and a packet that is no RTP packet, are dropped.
func (c *Conn) deliver(r *bufio.Reader) error {
	channel, data, err := wire.ReadInterleaved(r)
	if err != nil {
		return err
	}
	if uint32(channel)+1 != c.channel.Load() {
		return nil
	}
	p, err := wire.ParsePacket(data)
	if err != nil {
		return nil
	}
	select {
	case c.packets <- p:
		return nil
	case <-c.closing:
		return ErrClosed
	}
}
