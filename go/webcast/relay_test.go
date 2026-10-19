package webcast

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"greendrake.nz/go/camstream"
	"greendrake.nz/go/camstream/camstreamtest"
)

// opening starts relaying testKey off the server: what awaits the relay it opens, shut
// down with the test.
func opening(t *testing.T, server *httptest.Server, opts ...RelayOption) func() camstream.Monitor {
	opened := make(chan camstream.Monitor, 1)
	failed := make(chan error, 1)
	go func() {
		r, err := Relay(context.Background(), urlOf(server), testKey, opts...)
		if err != nil {
			failed <- err
			return
		}
		opened <- r
	}()
	return func() camstream.Monitor {
		t.Helper()
		select {
		case r := <-opened:
			t.Cleanup(r.ShutDown)
			return r
		case err := <-failed:
			t.Fatalf("the relay failed to open: %v", err)
		case <-time.After(2 * time.Second):
			t.Fatal("the relay did not open")
		}
		return nil
	}
}

// relayed relays testKey off the server, its stream open on a monitor carrying audio
// that sends sent after a frame ahead of any key frame: the relay, and the monitor.
func relayed(t *testing.T, sent []*camstream.Frame) (camstream.Monitor, *camstreamtest.Monitor) {
	t.Helper()
	server, cam, _ := streamServer(t)
	opened := opening(t, server)
	monitor := camstreamtest.NewMonitor()
	monitor.Audio = true
	cam.Opens(monitor)
	monitor.Frames <- videoFrame(9*time.Second, false)
	for _, f := range sent {
		monitor.Frames <- f
	}
	return opened(), monitor
}

// A relay yields the host's frames whole and in order, from a key frame on, and carries
// audio as the host's stream does.
func TestRelay(t *testing.T) {
	sent := []*camstream.Frame{videoFrame(10*time.Second, true), audioFrame(10*time.Second+20*time.Millisecond, 1), videoFrame(10*time.Second+40*time.Millisecond, false)}
	r, _ := relayed(t, sent)
	if !r.HasAudio() {
		t.Fatal("the relay has no audio")
	}
	for _, want := range sent {
		if got, err := r.Next(); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("relayed %+v, %v, want %+v", got, err, want)
		}
	}
}

// An opening waits out its host's silence for as long as the host's camera is away; a
// relay that plays and hears nothing for its quiet window takes its host for gone.
func TestRelayQuiet(t *testing.T) {
	const quiet = 50 * time.Millisecond
	server, cam, _ := streamServer(t)
	opened := opening(t, server, WithQuiet(quiet))
	// The camera stays away a few times the quiet window before its stream opens.
	time.Sleep(4 * quiet)
	monitor := camstreamtest.NewMonitor()
	cam.Opens(monitor)
	monitor.Frames <- videoFrame(10*time.Second, true)
	r := opened()
	if _, err := r.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Next(); err == nil {
		t.Fatal("a relay heard nothing and went on")
	}
}

// A relay ends with the host's stream.
func TestRelayEndsWithTheStream(t *testing.T) {
	r, monitor := relayed(t, []*camstream.Frame{videoFrame(10*time.Second, true)})
	if _, err := r.Next(); err != nil {
		t.Fatal(err)
	}
	monitor.End()
	if _, err := r.Next(); !errors.Is(err, errGone) {
		t.Fatalf("got %v", err)
	}
}

// Shutting a relay down ends its subscription on the host, whose stream then closes.
func TestRelayShutDown(t *testing.T) {
	r, monitor := relayed(t, []*camstream.Frame{videoFrame(10*time.Second, true)})
	r.ShutDown()
	within(t, "the host's stream to close with its relay", monitor.Shut)
}

// A relay fails on what is no frame of its stream.
func TestRelayTakesNothingButItsFrames(t *testing.T) {
	// A raw payload past its channel: its kind, flags and frame.
	keyframe := appendFrame([]byte{kindRaw, 0}, videoFrame(time.Second, true))
	// relay relays off a host that subscribes it on channel 0, and sends message.
	relay := func(kind websocket.MessageType, message []byte) (camstream.Monitor, error) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ws, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer ws.CloseNow()
			ctx := r.Context()
			if _, _, err := ws.Read(ctx); err != nil {
				return
			}
			ws.Write(ctx, websocket.MessageText, []byte(`{"op":"subbed","ch":0}`))
			ws.Write(ctx, kind, message)
			ws.Read(ctx)
		}))
		t.Cleanup(server.Close)
		return Relay(context.Background(), urlOf(server), testKey)
	}
	r, err := relay(websocket.MessageBinary, append([]byte{0}, keyframe...))
	if err != nil {
		t.Fatalf("a frame of the stream: %v", err)
	}
	r.ShutDown()
	for name, message := range map[string]struct {
		kind websocket.MessageType
		data []byte
	}{
		"another channel": {websocket.MessageBinary, append([]byte{1}, keyframe...)},
		"another kind":    {websocket.MessageBinary, append([]byte{0, kindVideo}, keyframe[1:]...)},
		"no frame":        {websocket.MessageBinary, []byte{0, kindRaw, 0, 1, 2}},
		"short":           {websocket.MessageBinary, []byte{0, kindRaw}},
		"no op":           {websocket.MessageText, []byte("{")},
	} {
		if r, err := relay(message.kind, message.data); err == nil {
			r.ShutDown()
			t.Errorf("%s: relayed", name)
		}
	}
}

func TestRelayFails(t *testing.T) {
	refusing, _ := startTestServer(t, func(string, bool) *camstream.Source { return nil })
	if _, err := Relay(context.Background(), urlOf(refusing), testKey); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("a refused stream: %v", err)
	}
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	if _, err := Relay(context.Background(), "ws://"+closed.Addr().String(), testKey); err == nil {
		t.Error("relayed off no host")
	}
	// A host whose stream never opens holds the opening until its context ends.
	server, _, _ := streamServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Relay(ctx, urlOf(server), testKey); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("an opening past its context: %v", err)
	}
}
