package dvrip

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"greendrake.nz/go/dvrip/dvriptest"
	"greendrake.nz/go/dvrip/internal/wire"
)

// waitFor polls until condition holds, for state another goroutine changes.
func waitFor(t *testing.T, what string, within time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %v", what)
}

func ended(s *Session) func() bool {
	return func() bool {
		select {
		case <-s.Done():
			return true
		default:
			return false
		}
	}
}

// hangingUp is a control connection whose device answers a request and hangs up before
// the request's write returns, so that the request finds its reply delivered and the
// session ended both.
type hangingUp struct {
	net.Conn
	device net.Conn
	s      *Session
}

func (h *hangingUp) Write(b []byte) (int, error) {
	go func() {
		h.device.Write(wire.AppendPacket(nil, wire.Header{Code: wire.ConfigGet + 1}, []byte(`{"Name":"X","Ret":100,"X":1}`+"\n\x00")))
		h.device.Close()
	}()
	<-h.s.done
	return len(b), nil
}

// A reply that arrived before the session ended answers its request.
func TestAReplyOutlivesTheSession(t *testing.T) {
	// Each time, a request that took the session's end over its reply has an even chance.
	for range 64 {
		client, device := net.Pipe()
		s := &Session{done: make(chan struct{}), media: map[net.Conn]struct{}{}}
		s.control = &hangingUp{Conn: client, device: device, s: s}
		go s.read(time.Hour)
		if v, err := s.query(context.Background(), wire.ConfigGet, "X"); err != nil || string(v) != "1" {
			t.Fatalf("got %s, %v", v, err)
		}
	}
}

func TestLoginRefusals(t *testing.T) {
	tests := map[string]struct {
		user, password string
		reply          map[string]any
		ret            Ret
		want           string
	}{
		"wrong password": {user: "admin", password: "nope", ret: 203},
		"unknown user":   {user: "nobody", password: "", ret: 205},
		"no session ID": {
			user: "admin", password: "",
			reply: map[string]any{"Ret": 100, "AliveInterval": 20},
			want:  `no "SessionID"`,
		},
		"unreadable session ID": {
			user: "admin", password: "",
			reply: map[string]any{"Ret": 100, "AliveInterval": 20, "SessionID": "session"},
			want:  "session ID",
		},
		"no keep-alive interval": {
			user: "admin", password: "",
			reply: map[string]any{"Ret": 100, "AliveInterval": 0, "SessionID": "0x1"},
			want:  "keep-alive interval",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			camera := dvriptest.New(t, "techage", dvriptest.Options{LoginReply: test.reply})
			_, err := Dial(context.Background(), camera.Address(), test.user, test.password)
			if test.ret != 0 {
				var retErr *RetError
				if !errors.As(err, &retErr) || retErr.Ret != test.ret || !retErr.IsCredentials() {
					t.Fatalf("got %v, want credentials refused with %d", err, test.ret)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want an error about %q", err, test.want)
			}
		})
	}
}

func TestDialGivesUpWhenCancelledDuringLogin(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{Silent: map[string]bool{"login": true}})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Dial(ctx, camera.Address(), "admin", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want the deadline", err)
	}
}

func TestDialFailsWhenTheDeviceHangsUp(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			wire.ReadPacket(conn)
			conn.Close()
		}
	}()
	if _, err := Dial(context.Background(), listener.Addr().String(), "admin", ""); !errors.Is(err, io.EOF) {
		t.Fatalf("got %v, want the connection's end", err)
	}
}

func TestDialAddsTheDefaultPort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// The dial itself is cancelled; what matters is the address it was given.
	_, err := Dial(ctx, "192.0.2.1", "admin", "")
	if err == nil || !strings.Contains(err.Error(), "192.0.2.1:"+DefaultPort) {
		t.Fatalf("got %v, want a dial of port %s", err, DefaultPort)
	}
}

func TestKeepAliveHoldsTheSession(t *testing.T) {
	t.Parallel()
	camera := dvriptest.New(t, "techage", dvriptest.Options{AliveInterval: 1})
	s := dial(t, camera)
	waitFor(t, "two keep-alives", 4*time.Second, func() bool {
		return len(camera.Commands("KeepAlive")) >= 2
	})
	if err := s.Err(); err != nil {
		t.Fatalf("session ended: %v", err)
	}
}

func TestSilentDeviceEndsTheSession(t *testing.T) {
	t.Parallel()
	camera := dvriptest.New(t, "techage", dvriptest.Options{AliveInterval: 1, Silent: map[string]bool{"KeepAlive": true}})
	s := dial(t, camera)
	waitFor(t, "the session to end", 5*time.Second, ended(s))
	if err := s.Err(); err == nil || !strings.Contains(err.Error(), "silent") {
		t.Fatalf("ended with %v, want the device found silent", err)
	}
}

func TestAbandonedRequestEndsTheSession(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{Silent: map[string]bool{"Hang": true}})
	s := dial(t, camera)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		waitFor(t, "the request to go out", time.Second, func() bool {
			return len(camera.Commands("Hang")) == 1
		})
		cancel()
	}()
	if _, err := s.Get(ctx, "Hang"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want the cancellation", err)
	}
	if !ended(s)() {
		t.Fatal("a request given up on must end the session")
	}
	if _, err := s.Get(context.Background(), "General.General"); !errors.Is(err, context.Canceled) {
		t.Fatalf("a request on an ended session got %v, want why it ended", err)
	}
}

func TestUnsolicitedMessageEndsTheSession(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{})
	s := dial(t, camera)
	camera.Push(1504)
	waitFor(t, "the session to end", time.Second, ended(s))
	if err := s.Err(); err == nil || !strings.Contains(err.Error(), "unsolicited message 1504") {
		t.Fatalf("ended with %v", err)
	}
}

func TestRepliesSpanningPackets(t *testing.T) {
	s := dial(t, dvriptest.New(t, "techage", dvriptest.Options{Chunk: 16}))
	general, err := General.Get(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if general.OverWrite != OverWriteOldest {
		t.Fatalf("decoded %+v", general)
	}
}

func TestCloseEndsTheSession(t *testing.T) {
	s := dial(t, dvriptest.New(t, "techage", dvriptest.Options{}))
	s.Close()
	if !errors.Is(s.Err(), ErrClosed) {
		t.Fatalf("ended with %v", s.Err())
	}
	if _, err := s.Get(context.Background(), "General.General"); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
	if _, err := s.Monitor(context.Background(), 0, Main); !errors.Is(err, ErrClosed) {
		t.Fatalf("a monitor on a closed session got %v", err)
	}
}
