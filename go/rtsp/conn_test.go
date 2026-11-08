package rtsp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"greendrake.nz/go/rtsp/internal/wire"
	"greendrake.nz/go/rtsp/rtsptest"
)

const (
	user     = "admin"
	password = "pass"
)

// units are three access units of H.265, the first a random access one whose slice
// takes fragmentation units.
var units = []rtsptest.Unit{
	{NALUs: [][]byte{vps, sps, append([]byte{19 << 1, 1}, bytes.Repeat([]byte{7}, 3000)...)}, PTS: 0},
	{NALUs: [][]byte{trail}, PTS: 40 * time.Millisecond},
	{NALUs: [][]byte{trail}, PTS: 80 * time.Millisecond},
}

func serve(t *testing.T, timeout int) *rtsptest.Camera {
	return rtsptest.Serve(t, rtsptest.Options{User: user, Password: password, Timeout: timeout, Streams: map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", units), "/1": rtsptest.Video("H265", units)}})
}

func dial(t *testing.T, address, password string) *Conn {
	t.Helper()
	c, err := Dial(context.Background(), address, user, password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

// transports are the transports a stream plays by, by name.
var transports = map[string]Transport{"UDP": UDP, "TCP": TCP}

// play plays the camera's stream at path on c by transport.
func play(t *testing.T, c *Conn, path string, transport Transport) *Session {
	t.Helper()
	ctx := context.Background()
	d, err := c.Describe(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Play(ctx, d, d.Medias[0], transport)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// playing is a connection playing the camera's stream at /0 by transport, and its session.
func playing(t *testing.T, camera *rtsptest.Camera, transport Transport) (*Conn, *Session) {
	t.Helper()
	c := dial(t, camera.Address(), password)
	return c, play(t, c, "/0", transport)
}

// unitsArrive fails t unless the session's next units are the stream's, whole and timed.
func unitsArrive(t *testing.T, s *Session) {
	t.Helper()
	var h H265
	clock := Media{ClockRate: 90000}.Clock()
	for _, want := range units {
		var got [][]byte
		var timestamp uint32
		for got == nil {
			select {
			case p := <-s.Packets():
				got, timestamp = h.Push(p)
			case <-time.After(2 * time.Second):
				t.Fatal("no packet came")
			}
		}
		if at, err := clock.Time(timestamp); err != nil || !equal(got, want.NALUs) || at != want.PTS {
			t.Fatalf("unit at %v (%v) of %d NAL units, want at %v", at, err, len(got), want.PTS)
		}
	}
}

// stillPlaying fails t unless a packet comes to s once those it holds are read.
func stillPlaying(t *testing.T, s *Session) {
	t.Helper()
	for len(s.Packets()) > 0 {
		<-s.Packets()
	}
	select {
	case _, live := <-s.Packets():
		if !live {
			t.Fatalf("the session ended: %v", s.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no packet came")
	}
}

// A stream is described, set up and played as the camera asks to be authenticated, and its
// units arrive whole.
func TestPlay(t *testing.T) {
	for name, transport := range transports {
		t.Run(name, func(t *testing.T) {
			camera := serve(t, 0)
			_, s := playing(t, camera, transport)
			// Every request after the first answers the challenge the first was refused with.
			for method, want := range map[string]int{"DESCRIBE": 2, "SETUP": 1, "PLAY": 1} {
				if got := camera.Requests(method); got != want {
					t.Fatalf("%s asked %d times", method, got)
				}
			}
			unitsArrive(t, s)
		})
	}
}

// Streams play at once on one connection, each a session of its own: closing one tears it
// down alone, and leaves the other playing.
func TestSessionsOnOneConnection(t *testing.T) {
	for name, transport := range transports {
		t.Run(name, func(t *testing.T) {
			camera := serve(t, 0)
			c := dial(t, camera.Address(), password)
			main, extra := play(t, c, "/0", transport), play(t, c, "/1", transport)
			unitsArrive(t, main)
			unitsArrive(t, extra)
			main.Close()
			for range main.Packets() {
			}
			if !errors.Is(main.Err(), ErrClosed) {
				t.Fatalf("ended for %v", main.Err())
			}
			tornDown(t, camera)
			stillPlaying(t, extra)
			if got := camera.Requests("TEARDOWN"); got != 1 {
				t.Fatalf("%d teardowns", got)
			}
			select {
			case <-c.Done():
				t.Fatalf("the connection ended with the session: %v", c.Err())
			default:
			}
		})
	}
}

// A camera that serves one connection at a time leaves a second unanswered, which is why
// every stream plays on the one connection.
func TestSerialCamera(t *testing.T) {
	camera := rtsptest.Serve(t, rtsptest.Options{User: user, Password: password, Serial: true, Streams: map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", units), "/1": rtsptest.Video("H265", units)}})
	first := dial(t, camera.Address(), password)
	if err := first.Options(context.Background(), "/0"); err != nil {
		t.Fatal(err)
	}
	if err := dial(t, camera.Address(), password).Options(context.Background(), "/0"); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("a second connection got %v", err)
	}
	unitsArrive(t, play(t, first, "/0", UDP))
	unitsArrive(t, play(t, first, "/1", UDP))
}

// A stream played over UDP is let in by a NAT between: a packet goes from each of the
// client's ports to the camera's of its kind.
func TestPlayOpensTheWay(t *testing.T) {
	camera := serve(t, 0)
	playing(t, camera, UDP)
	eventually(t, 2*time.Second, "the camera heard from neither port, or one", func() bool {
		return camera.Heard("RTP") > 0 && camera.Heard("RTCP") > 0
	})
}

// eventually fails t with failure unless done comes true within d.
func eventually(t *testing.T, d time.Duration, failure string, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(d); !done(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal(failure)
		}
	}
}

func equal(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

func TestRefusedPassword(t *testing.T) {
	c := dial(t, serve(t, 0).Address(), "wrong")
	var refused *StatusError
	if _, err := c.Describe(context.Background(), "/0"); !errors.As(err, &refused) || refused.Status != 401 || refused.Method != "DESCRIBE" {
		t.Fatalf("got %v", err)
	}
	if _, err := c.Describe(context.Background(), "/9"); !errors.As(err, &refused) || refused.Status != 401 {
		t.Fatalf("got %v", err)
	}
}

// A session is kept alive as its timeout asks.
func TestKeepAlive(t *testing.T) {
	camera := serve(t, 1)
	playing(t, camera, UDP)
	eventually(t, 3*time.Second, "the session was not kept alive", func() bool { return camera.Requests("OPTIONS") >= 2 })
}

// A request abandoned with its context leaves the connection to the next, its answer
// dropped when it comes.
func TestRequestAbandonedWithItsContext(t *testing.T) {
	received := make(chan struct{})
	address := server(t, func(conn net.Conn) {
		r := bufio.NewReader(conn)
		abandoned, err := wire.Read(r)
		if err != nil {
			return
		}
		close(received)
		next, err := wire.Read(r)
		if err != nil {
			return
		}
		conn.Write((&wire.Message{Status: 404, Reason: "Not Found", Header: wire.Header{{"CSeq", abandoned.Header.Get("CSeq")}}}).Bytes())
		conn.Write((&wire.Message{Status: 200, Reason: "OK", Header: wire.Header{{"CSeq", next.Header.Get("CSeq")}}}).Bytes())
		bufio.NewReader(conn).ReadByte()
	})
	c := dial(t, address, password)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-received
		cancel()
	}()
	if err := c.Options(ctx, "/0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	select {
	case <-c.Done():
		t.Fatalf("the connection ended: %v", c.Err())
	default:
	}
	if err := c.Options(context.Background(), "/0"); err != nil {
		t.Fatalf("the next request got %v", err)
	}
}

// A request the server leaves unanswered ends the connection.
func TestNoAnswer(t *testing.T) {
	camera := serve(t, 0)
	c := dial(t, camera.Address(), password)
	camera.Hold()
	if err := c.Options(context.Background(), "/0"); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("got %v", err)
	}
	<-c.Done()
}

// tornDown fails t unless the camera is asked to tear its stream down.
func tornDown(t *testing.T, camera *rtsptest.Camera) {
	t.Helper()
	eventually(t, 2*time.Second, "no teardown", func() bool { return camera.Requests("TEARDOWN") > 0 })
}

// Closing a connection playing tears its stream down, and ends its session.
func TestClose(t *testing.T) {
	for name, transport := range transports {
		t.Run(name, func(t *testing.T) {
			camera := serve(t, 0)
			c, s := playing(t, camera, transport)
			c.Close()
			for range s.Packets() {
			}
			if !errors.Is(c.Err(), ErrClosed) || !errors.Is(s.Err(), ErrClosed) {
				t.Fatalf("ended for %v, the session for %v", c.Err(), s.Err())
			}
			tornDown(t, camera)
		})
	}
}

// A session whose reader has fallen behind is torn down all the same, with the connection
// or on its own.
func TestCloseBehind(t *testing.T) {
	// One unit of more packets than a session holds for its reader.
	big := append([]byte{19 << 1, 1}, make([]byte, 2*packetBuffer*1400)...)
	for name, transport := range transports {
		for closing, close := range map[string]func(*Conn, *Session){"the connection": func(c *Conn, _ *Session) { c.Close() }, "the session": func(_ *Conn, s *Session) { s.Close() }} {
			t.Run(name+", "+closing, func(t *testing.T) {
				camera := rtsptest.Serve(t, rtsptest.Options{User: user, Password: password, Streams: map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", []rtsptest.Unit{{NALUs: [][]byte{big}}})}})
				c, s := playing(t, camera, transport)
				eventually(t, 5*time.Second, "the session did not fall behind", func() bool { return len(s.Packets()) == packetBuffer })
				close(c, s)
				tornDown(t, camera)
			})
		}
	}
}

// An interleaved session a buffer behind holds up neither the connection's answers nor
// its other sessions.
func TestBehindHoldsUpNothing(t *testing.T) {
	big := append([]byte{19 << 1, 1}, make([]byte, 2*packetBuffer*1400)...)
	camera := rtsptest.Serve(t, rtsptest.Options{User: user, Password: password, Streams: map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", []rtsptest.Unit{{NALUs: [][]byte{big}}}), "/1": rtsptest.Video("H265", units)}})
	c := dial(t, camera.Address(), password)
	behind, other := play(t, c, "/0", TCP), play(t, c, "/1", TCP)
	eventually(t, 5*time.Second, "the session did not fall behind", func() bool { return len(behind.Packets()) == packetBuffer })
	if err := c.Options(context.Background(), "/0"); err != nil {
		t.Fatalf("the connection's answer was held up: %v", err)
	}
	stillPlaying(t, other)
}

// A connection that has ended plays nothing, and holds no ports for it.
func TestPlayEnded(t *testing.T) {
	camera := serve(t, 0)
	c := dial(t, camera.Address(), password)
	d, err := c.Describe(context.Background(), "/0")
	if err != nil {
		t.Fatal(err)
	}
	camera.Close()
	<-c.Done()
	if s, err := c.Play(context.Background(), d, d.Medias[0], UDP); s != nil || err == nil || err != c.Err() {
		t.Fatalf("got %v, %v", s, err)
	}
	if len(c.sessions) != 0 {
		t.Fatal("a session was kept")
	}
}

// A connection the server ends is closed this end too.
func TestServerEnds(t *testing.T) {
	camera := serve(t, 0)
	c := dial(t, camera.Address(), password)
	camera.Close()
	<-c.Done()
	if _, err := c.nc.Write([]byte{0}); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("the socket is open: %v", err)
	}
}

func TestDialAddsTheDefaultPort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// The dial itself is cancelled; what matters is the address it was given.
	_, err := Dial(ctx, "192.0.2.1", user, password)
	if err == nil || !strings.Contains(err.Error(), "192.0.2.1:"+defaultPort) {
		t.Fatalf("got %v, want a dial of port %s", err, defaultPort)
	}
}

func TestDialFails(t *testing.T) {
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	if _, err := Dial(context.Background(), closed.Addr().String(), user, password); err == nil {
		t.Fatal("dialled nothing")
	}
}

// A server's own requests are answered: OPTIONS, to see the client is there, and
// nothing else. An answer to no request waited for is dropped.
func TestServerRequests(t *testing.T) {
	answers := make(chan []*wire.Message, 1)
	address := server(t, func(conn net.Conn) {
		r := bufio.NewReader(conn)
		conn.Write([]byte("RTSP/1.0 200 OK\r\nCSeq: 7\r\n\r\nOPTIONS * RTSP/1.0\r\nCSeq: 1\r\n\r\nANNOUNCE rtsp://cam/0 RTSP/1.0\r\nCSeq: 2\r\n\r\n"))
		var got []*wire.Message
		for range 2 {
			m, err := wire.Read(r)
			if err != nil {
				break
			}
			got = append(got, m)
		}
		answers <- got
	})
	dial(t, address, password)
	got := <-answers
	if len(got) != 2 || got[0].Status != 200 || got[0].Header.Get("CSeq") != "1" || got[1].Status != 501 || got[1].Header.Get("CSeq") != "2" {
		t.Fatalf("answered %+v", got)
	}
}

// A setup the server refuses fails the play, in its words.
func TestSetupRefused(t *testing.T) {
	camera := serve(t, 0)
	c := dial(t, camera.Address(), password)
	d, err := c.Describe(context.Background(), "/0")
	if err != nil {
		t.Fatal(err)
	}
	media := d.Medias[0]
	media.Control = "rtsp://" + camera.Address() + "/9/trackID=0"
	_, err = c.Play(context.Background(), d, media, UDP)
	if err == nil || err.Error() != "rtsp: SETUP refused: 404 Not Found" {
		t.Fatalf("got %v", err)
	}
	if len(c.sessions) != 0 {
		t.Fatal("a session refused was kept")
	}
}

// A play abandoned with its context while its setup is out still takes the setup's answer,
// which alone names the session, and tears the session down.
func TestAbandonedSetupIsTornDown(t *testing.T) {
	received, cancelled := make(chan struct{}), make(chan struct{})
	teardown := make(chan *wire.Message, 1)
	address := server(t, func(conn net.Conn) {
		r := bufio.NewReader(conn)
		setup, err := wire.Read(r)
		if err != nil {
			return
		}
		close(received)
		<-cancelled
		conn.Write((&wire.Message{Status: 200, Reason: "OK", Header: wire.Header{{"CSeq", setup.Header.Get("CSeq")}, {"Session", "s1"}, {"Transport", "RTP/AVP/TCP;unicast;interleaved=0-1"}}}).Bytes())
		if m, err := wire.Read(r); err == nil {
			teardown <- m
		}
	})
	c := dial(t, address, password)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-received
		cancel()
		close(cancelled)
	}()
	uri := "rtsp://" + address + "/0"
	if _, err := c.Play(ctx, &Description{Control: uri}, Media{Control: uri}, TCP); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	select {
	case m := <-teardown:
		if m.Method != "TEARDOWN" || m.Header.Get("Session") != "s1" {
			t.Fatalf("sent %s in session %q", m.Method, m.Header.Get("Session"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the session set up was left to the server")
	}
}

// A server that puts a session on another's interleaved channel fails its play, rather
// than mix their packets.
func TestInterleavedChannelTaken(t *testing.T) {
	address := server(t, func(conn net.Conn) {
		r := bufio.NewReader(conn)
		for setups := 0; ; {
			req, err := wire.Read(r)
			if err != nil {
				return
			}
			header := wire.Header{{"CSeq", req.Header.Get("CSeq")}}
			if req.Method == "SETUP" {
				setups++
				header = append(header, [2]string{"Session", fmt.Sprintf("s%d", setups)}, [2]string{"Transport", "RTP/AVP/TCP;unicast;interleaved=0-1"})
			}
			conn.Write((&wire.Message{Status: 200, Reason: "OK", Header: header}).Bytes())
		}
	})
	c := dial(t, address, password)
	uri := "rtsp://" + address + "/0"
	if _, err := c.Play(context.Background(), &Description{Control: uri}, Media{Control: uri}, TCP); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Play(context.Background(), &Description{Control: uri}, Media{Control: uri}, TCP); err == nil || err.Error() != "rtsp: the server put the session on interleaved channel 0, another session's" {
		t.Fatalf("got %v", err)
	}
}

// A setup answered with no ports, or no channel, for its packets fails the play.
func TestSetupAnsweredAmiss(t *testing.T) {
	for transport, want := range map[Transport]string{
		UDP: `rtsp: no server_port in "RTP/AVP;unicast"`,
		TCP: `rtsp: no interleaved in "RTP/AVP;unicast"`,
	} {
		address := server(t, func(conn net.Conn) {
			r := bufio.NewReader(conn)
			for {
				req, err := wire.Read(r)
				if err != nil {
					return
				}
				conn.Write((&wire.Message{Status: 200, Reason: "OK", Header: wire.Header{{"CSeq", req.Header.Get("CSeq")}, {"Session", "1"}, {"Transport", "RTP/AVP;unicast"}}}).Bytes())
			}
		})
		c := dial(t, address, password)
		uri := "rtsp://" + address + "/0"
		if _, err := c.Play(context.Background(), &Description{Control: uri}, Media{Control: uri}, transport); err == nil || err.Error() != want {
			t.Errorf("got %v, want %s", err, want)
		}
	}
}

// server serves one connection, as serve does, until the test ends: the address it
// listens at.
func server(t *testing.T, serve func(conn net.Conn)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		serve(conn)
	}()
	return listener.Addr().String()
}

// passedOn fails t unless the next packet s passes on is sent.
func passedOn(t *testing.T, s *Session, sent wire.Packet) {
	t.Helper()
	select {
	case p := <-s.Packets():
		if p.Sequence != sent.Sequence || !bytes.Equal(p.Payload, sent.Payload) {
			t.Fatalf("passed on %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no packet passed on")
	}
}

// opened is a session readied on c to receive by transport, as a play readies it.
func opened(t *testing.T, c *Conn, transport Transport) *Session {
	t.Helper()
	s := &Session{conn: c, packets: make(chan Packet, packetBuffer), ended: make(chan struct{})}
	if _, err := c.open(s, transport); err != nil {
		t.Fatal(err)
	}
	return s
}

// Of what arrives interleaved, the RTP packets of a session's channel alone are passed on
// to it: not RTCP, not what is no RTP packet, and not another session's.
func TestOnlyTheStreamsPackets(t *testing.T) {
	sent := wire.Packet{PayloadType: 96, Sequence: 1, Payload: []byte{1}}
	other := wire.Packet{PayloadType: 96, Sequence: 2, Payload: []byte{2}}
	// Closed once the sessions are on channels 0 and 2.
	set := make(chan struct{})
	c := dial(t, server(t, func(conn net.Conn) {
		<-set
		var b []byte
		b = wire.AppendInterleaved(b, 1, []byte{0x80, 200, 0, 1})
		b = wire.AppendInterleaved(b, 0, []byte{1, 2, 3})
		b = wire.AppendInterleaved(b, 2, wire.AppendPacket(nil, other, 1))
		b = wire.AppendInterleaved(b, 0, wire.AppendPacket(nil, sent, 1))
		conn.Write(b)
		bufio.NewReader(conn).ReadByte()
	}), password)
	first, second := opened(t, c, TCP), opened(t, c, TCP)
	close(set)
	passedOn(t, first, sent)
	passedOn(t, second, other)
}

// Of what arrives at a session's RTP port, the server's RTP packets alone are passed on:
// not what comes from elsewhere, and not what is no RTP packet.
func TestOnlyTheServersPackets(t *testing.T) {
	c := dial(t, server(t, func(conn net.Conn) { bufio.NewReader(conn).ReadByte() }), password)
	s := opened(t, c, UDP)
	rtp := s.rtp
	sent := wire.Packet{PayloadType: 96, Sequence: 1, Payload: []byte{1}}
	for _, packet := range []struct {
		from string
		b    []byte
	}{
		{"127.0.0.2", wire.AppendPacket(nil, wire.Packet{PayloadType: 96, Sequence: 2, Payload: []byte{2}}, 1)},
		{"127.0.0.1", []byte{1, 2, 3}},
		{"127.0.0.1", wire.AppendPacket(nil, sent, 1)},
	} {
		conn, err := net.DialUDP("udp", &net.UDPAddr{IP: net.ParseIP(packet.from)}, &net.UDPAddr{IP: c.server, Port: rtp.LocalAddr().(*net.UDPAddr).Port})
		if err != nil {
			t.Fatal(err)
		}
		conn.Write(packet.b)
		conn.Close()
	}
	passedOn(t, s, sent)
}

func TestSessionField(t *testing.T) {
	if id, timeout, err := parseSession("12345678;timeout=30"); err != nil || id != "12345678" || timeout != 30*time.Second {
		t.Fatalf("%q %v, %v", id, timeout, err)
	}
	if _, timeout, err := parseSession("abc"); err != nil || timeout != defaultSessionTimeout {
		t.Fatalf("%v, %v", timeout, err)
	}
	for _, field := range []string{"", ";timeout=30", "abc;timeout=0", "abc;timeout=x"} {
		if _, _, err := parseSession(field); err == nil {
			t.Errorf("session %q read", field)
		}
	}
}
