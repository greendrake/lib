package rtsp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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
	return rtsptest.Serve(t, rtsptest.Options{User: user, Password: password, Timeout: timeout, Streams: map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", units)}})
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

// playing is a connection playing the camera's stream by transport.
func playing(t *testing.T, camera *rtsptest.Camera, transport Transport) *Conn {
	t.Helper()
	c := dial(t, camera.Address(), password)
	ctx := context.Background()
	d, err := c.Describe(ctx, "/0")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Play(ctx, d, d.Medias[0], transport); err != nil {
		t.Fatal(err)
	}
	return c
}

// A stream is described, set up and played as the camera asks to be authenticated, and its
// units arrive whole.
func TestPlay(t *testing.T) {
	for name, transport := range transports {
		t.Run(name, func(t *testing.T) {
			camera := serve(t, 0)
			c := playing(t, camera, transport)
			// Every request after the first answers the challenge the first was refused with.
			for method, want := range map[string]int{"DESCRIBE": 2, "SETUP": 1, "PLAY": 1} {
				if got := camera.Requests(method); got != want {
					t.Fatalf("%s asked %d times", method, got)
				}
			}
			var h H265
			clock := Media{ClockRate: 90000}.Clock()
			for _, want := range units {
				var got [][]byte
				var timestamp uint32
				for got == nil {
					select {
					case p := <-c.Packets():
						got, timestamp = h.Push(p)
					case <-time.After(2 * time.Second):
						t.Fatal("no packet came")
					}
				}
				if at, err := clock.Time(timestamp); err != nil || !equal(got, want.NALUs) || at != want.PTS {
					t.Fatalf("unit at %v (%v) of %d NAL units, want at %v", at, err, len(got), want.PTS)
				}
			}
		})
	}
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

// A request abandoned with its context ends the connection.
func TestRequestEndsWithItsContext(t *testing.T) {
	camera := serve(t, 0)
	c := dial(t, camera.Address(), password)
	camera.Hold()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Describe(ctx, "/0"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	<-c.Done()
}

// tornDown fails t unless the camera is asked to tear its stream down.
func tornDown(t *testing.T, camera *rtsptest.Camera) {
	t.Helper()
	eventually(t, 2*time.Second, "no teardown", func() bool { return camera.Requests("TEARDOWN") > 0 })
}

// Closing a connection playing tears its stream down.
func TestClose(t *testing.T) {
	for name, transport := range transports {
		t.Run(name, func(t *testing.T) {
			camera := serve(t, 0)
			c := playing(t, camera, transport)
			c.Close()
			for range c.Packets() {
			}
			if !errors.Is(c.Err(), ErrClosed) {
				t.Fatalf("ended for %v", c.Err())
			}
			tornDown(t, camera)
		})
	}
}

// A connection whose reader has fallen behind tears its stream down all the same.
func TestCloseBehind(t *testing.T) {
	// One unit of more packets than the connection holds for its reader.
	big := append([]byte{19 << 1, 1}, make([]byte, 2*packetBuffer*1400)...)
	for name, transport := range transports {
		t.Run(name, func(t *testing.T) {
			camera := rtsptest.Serve(t, rtsptest.Options{User: user, Password: password, Streams: map[string]rtsptest.Stream{"/0": rtsptest.Video("H265", []rtsptest.Unit{{NALUs: [][]byte{big}}})}})
			c := playing(t, camera, transport)
			eventually(t, 5*time.Second, "the connection did not fall behind", func() bool { return len(c.Packets()) == packetBuffer })
			c.Close()
			tornDown(t, camera)
		})
	}
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
	if err := c.Play(context.Background(), d, d.Medias[0], UDP); err == nil || err != c.Err() {
		t.Fatalf("got %v", err)
	}
	if c.rtp != nil {
		t.Fatal("the ports were kept")
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
	err = c.Play(context.Background(), d, media, UDP)
	if err == nil || err.Error() != "rtsp: SETUP refused: 404 Not Found" {
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
		if err := c.Play(context.Background(), &Description{Control: uri}, Media{Control: uri}, transport); err == nil || err.Error() != want {
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

// passedOn fails t unless the next packet c passes on is sent.
func passedOn(t *testing.T, c *Conn, sent wire.Packet) {
	t.Helper()
	select {
	case p := <-c.Packets():
		if p.Sequence != sent.Sequence || !bytes.Equal(p.Payload, sent.Payload) {
			t.Fatalf("passed on %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no packet passed on")
	}
}

// Of what arrives interleaved, the RTP packets of the stream played alone are passed on:
// not RTCP, and not what is no RTP packet.
func TestOnlyTheStreamsPackets(t *testing.T) {
	sent := wire.Packet{PayloadType: 96, Sequence: 1, Payload: []byte{1}}
	// Closed once the connection plays on channel 0.
	set := make(chan struct{})
	c := dial(t, server(t, func(conn net.Conn) {
		<-set
		var b []byte
		b = wire.AppendInterleaved(b, 1, []byte{0x80, 200, 0, 1})
		b = wire.AppendInterleaved(b, 0, []byte{1, 2, 3})
		b = wire.AppendInterleaved(b, 0, wire.AppendPacket(nil, sent, 1))
		conn.Write(b)
		bufio.NewReader(conn).ReadByte()
	}), password)
	c.channel.Store(1)
	close(set)
	passedOn(t, c, sent)
}

// Of what arrives at the RTP port, the server's RTP packets alone are passed on: not what
// comes from elsewhere, and not what is no RTP packet.
func TestOnlyTheServersPackets(t *testing.T) {
	c := dial(t, server(t, func(conn net.Conn) { bufio.NewReader(conn).ReadByte() }), password)
	rtp, rtcp, err := wire.ListenPair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.receive(rtp, rtcp); err != nil {
		t.Fatal(err)
	}
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
	passedOn(t, c, sent)
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
