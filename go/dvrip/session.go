// Package dvrip is a client for DVRIP, the protocol of Xiongmai-based cameras and
// recorders (also known as Sofia, NetSurveillance or XMEye): logging in, reading and
// writing configuration, and pulling live streams with their audio.
package dvrip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"greendrake.nz/go/dvrip/internal/wire"
)

// DefaultPort is the TCP port devices serve the protocol on.
const DefaultPort = "34567"

// ErrClosed is why a session its owner closed has ended.
var ErrClosed = errors.New("dvrip: session closed")

const (
	defaultStallLimit = 5 * time.Second
	// How many keep-alive intervals the control connection may stay silent before the
	// session is given up. A keep-alive is answered well within one.
	livenessIntervals = 3
)

// Session is one login on a device. Its control connection carries the login, the
// keep-alive and every request; each media operation it starts runs on a connection
// of its own, claimed for the session. That is the vendor's design, and the one that
// survives stopping a stream: the device ends a media operation by closing the
// connection the media was on, and on the control connection that would end the
// login with it.
type Session struct {
	address string
	id      uint32
	stall   time.Duration
	control net.Conn

	// Serialises requests, so that whatever arrives is the outstanding request's reply,
	// and guards sequence.
	requestMutex sync.Mutex
	sequence     uint32
	// Where the reader delivers the next control message. Nil while no request waits,
	// which makes a message unsolicited.
	pendingMutex sync.Mutex
	pending      chan wire.Packet

	done     chan struct{}
	failOnce sync.Once
	err      error

	// The open media connections, which end with the session. Nil once it has ended.
	mediaMutex sync.Mutex
	media      map[net.Conn]struct{}
}

// Option configures a session.
type Option func(*Session)

// WithStallLimit sets how long a media connection may go without a packet before
// reading from it fails. The default is 5 s.
func WithStallLimit(d time.Duration) Option {
	return func(s *Session) {
		s.stall = d
	}
}

// Dial connects to the device at address, "host" or "host:port", and logs in. ctx
// bounds the connecting and the login only; the session lasts until Close or until
// the device is lost.
func Dial(ctx context.Context, address, user, password string, options ...Option) (*Session, error) {
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, DefaultPort)
	}
	s := &Session{
		address: address,
		stall:   defaultStallLimit,
		done:    make(chan struct{}),
		media:   make(map[net.Conn]struct{}),
	}
	for _, option := range options {
		option(s)
	}
	var alive time.Duration
	err := connect(ctx, address, func(conn net.Conn) error {
		s.control = conn
		var err error
		alive, err = s.login(user, password)
		return err
	})
	if err != nil {
		return nil, err
	}
	go s.read(alive)
	go s.keepAlive(alive)
	return s, nil
}

// connect dials address and runs handshake on the new connection. Cancelling ctx
// aborts both, the handshake by closing the connection under it, and a failed
// handshake leaves the connection closed.
func connect(ctx context.Context, address string, handshake func(conn net.Conn) error) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() {
		conn.Close()
	})
	err = handshake(conn)
	if !stop() {
		return ctx.Err()
	}
	if err != nil {
		conn.Close()
	}
	return err
}

// readWithin reads from conn with a deadline d away, reporting a missed deadline as
// what having gone silent.
func readWithin(conn net.Conn, d time.Duration, what string, read func(io.Reader) (wire.Packet, error)) (wire.Packet, error) {
	if err := conn.SetReadDeadline(time.Now().Add(d)); err != nil {
		return wire.Packet{}, err
	}
	p, err := read(conn)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return wire.Packet{}, fmt.Errorf("dvrip: %s silent for %v", what, d)
	}
	return p, err
}

// login runs before the reader does, so it reads its reply itself.
func (s *Session) login(user, password string) (time.Duration, error) {
	body, err := wire.EncodeJSON(map[string]string{
		"EncryptType": "MD5",
		"LoginType":   "DVRIP-Web",
		"PassWord":    wire.SofiaHash(password),
		"UserName":    user,
	})
	if err != nil {
		return 0, err
	}
	if err := s.send(wire.Login, body); err != nil {
		return 0, err
	}
	p, err := wire.ReadMessage(s.control)
	if err != nil {
		return 0, err
	}
	r, err := decodeReply(p.Data, "login")
	if err != nil {
		return 0, err
	}
	id, err := decodeAs[string](r.field("SessionID"))
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseUint(id, 0, 32)
	if err != nil {
		return 0, fmt.Errorf("dvrip: login: session ID %q: %w", id, err)
	}
	s.id = uint32(parsed)
	seconds, err := decodeAs[int](r.field("AliveInterval"))
	if err != nil {
		return 0, err
	}
	if seconds <= 0 {
		return 0, fmt.Errorf("dvrip: login: keep-alive interval %d", seconds)
	}
	return time.Duration(seconds) * time.Second, nil
}

// read delivers each control message to the request waiting for it, for as long as
// the device keeps talking.
func (s *Session) read(alive time.Duration) {
	for {
		p, err := readWithin(s.control, livenessIntervals*alive, "device", wire.ReadMessage)
		if err != nil {
			s.fail(err)
			return
		}
		s.pendingMutex.Lock()
		replies := s.pending
		s.pending = nil
		s.pendingMutex.Unlock()
		if replies == nil {
			s.fail(fmt.Errorf("dvrip: unsolicited message %d", p.Code))
			return
		}
		replies <- p
	}
}

func (s *Session) keepAlive(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			// No deadline of its own: a device that stops answering trips the liveness
			// limit, which ends the session and this request with it.
			if _, err := s.command(context.Background(), wire.KeepAlive, "KeepAlive", nil); err != nil {
				s.fail(err)
				return
			}
		}
	}
}

// send writes one packet to the control connection. Callers hold requestMutex, except
// the login, which runs alone.
func (s *Session) send(c wire.Code, body []byte) error {
	data := wire.AppendPacket(nil, wire.Header{Session: s.id, Sequence: s.sequence, Code: c}, body)
	s.sequence++
	_, err := s.control.Write(data)
	return err
}

// request sends one control message and returns the reply's data.
func (s *Session) request(ctx context.Context, c wire.Code, body []byte) ([]byte, error) {
	s.requestMutex.Lock()
	defer s.requestMutex.Unlock()
	replies := make(chan wire.Packet, 1)
	s.pendingMutex.Lock()
	s.pending = replies
	s.pendingMutex.Unlock()
	// A request given up on while its reply is outstanding leaves no telling that reply
	// from the next request's, so giving up on one ends the session.
	stop := context.AfterFunc(ctx, func() {
		s.fail(context.Cause(ctx))
	})
	defer stop()
	if err := s.send(c, body); err != nil {
		s.fail(err)
	}
	select {
	case p := <-replies:
		return p.Data, nil
	case <-s.done:
		// A device can answer and hang up at once, as on a reboot: a reply delivered
		// before the session ended is still the request's.
		select {
		case p := <-replies:
			return p.Data, nil
		default:
			return nil, s.err
		}
	}
}

// commandBody is the JSON body every command shares: its name, the session, and what
// it carries, if anything, under its name.
func (s *Session) commandBody(name string, payload any) ([]byte, error) {
	body := map[string]any{
		"Name":      name,
		"SessionID": fmt.Sprintf("0x%08X", s.id),
	}
	if payload != nil {
		body[name] = payload
	}
	return wire.EncodeJSON(body)
}

// command sends a JSON command on the control connection and returns its reply.
func (s *Session) command(ctx context.Context, c wire.Code, name string, payload any) (reply, error) {
	body, err := s.commandBody(name, payload)
	if err != nil {
		return reply{}, err
	}
	data, err := s.request(ctx, c, body)
	if err != nil {
		return reply{}, err
	}
	return decodeReply(data, name)
}

// query sends a named command that carries nothing and returns what the reply carries
// under the same name.
func (s *Session) query(ctx context.Context, c wire.Code, name string) (json.RawMessage, error) {
	r, err := s.command(ctx, c, name, nil)
	if err != nil {
		return nil, err
	}
	return r.field(name)
}

// fail ends the session for the first reason given.
func (s *Session) fail(err error) {
	s.failOnce.Do(func() {
		s.err = err
		close(s.done)
		s.control.Close()
		s.mediaMutex.Lock()
		for conn := range s.media {
			conn.Close()
		}
		s.media = nil
		s.mediaMutex.Unlock()
	})
}

// failure prefers the reason the session ended to an error a connection reported
// because it did.
func (s *Session) failure(err error) error {
	select {
	case <-s.done:
		return s.err
	default:
		return err
	}
}

// Close ends the session and every media operation on it.
func (s *Session) Close() {
	s.fail(ErrClosed)
}

// Done is closed when the session ends.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Err reports why the session ended, or nil while it has not.
func (s *Session) Err() error {
	return s.failure(nil)
}
