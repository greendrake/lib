package apiserver

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func get(l listener) (string, error) {
	resp, err := http.Get("http://" + l.ln.Addr().String() + "/")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

// TestProcess_Lifecycle drives the whole lifecycle: every listener serves,
// SIGTERM drains in order — stop while the listeners still answer, the
// listeners' shutdown, release once they no longer do — and Run returns only
// after the background work release unblocks has finished.
func TestProcess_Lifecycle(t *testing.T) {
	p := NewProcess("testapp", 5*time.Second, 0)
	answer := func(label string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, label) })
	}
	if err := p.Listen("first", "0", answer("first")); err != nil {
		t.Fatal(err)
	}
	// One the caller has bound itself.
	bound, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p.Serve("second", bound, answer("second"))

	var mu sync.Mutex
	var steps []string
	step := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		steps = append(steps, s)
	}
	released := make(chan struct{})
	p.Go(func() {
		<-released
		step("background")
	})

	var stopSawDraining bool
	var stopServed, releaseServed, runErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		runErr = p.Run(func() {
			stopSawDraining = p.Draining().Load()
			_, stopServed = get(p.listeners[0])
			step("stop")
		}, func() {
			_, releaseServed = get(p.listeners[0])
			step("release")
			close(released)
		})
	}()

	for _, l := range p.listeners {
		body, err := get(l)
		if err != nil || body != l.label {
			t.Fatalf("%s: got %q, %v", l.label, body, err)
		}
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after SIGTERM")
	}

	if runErr != nil {
		t.Fatalf("a signalled drain is no failure: %v", runErr)
	}
	if !stopSawDraining {
		t.Fatal("draining must be flipped before stop runs")
	}
	if stopServed != nil {
		t.Fatalf("listeners must still serve while stop runs: %v", stopServed)
	}
	if releaseServed == nil {
		t.Fatal("listeners must be shut down before release runs")
	}
	if want := []string{"stop", "release", "background"}; !slices.Equal(steps, want) {
		t.Fatalf("drain steps: got %v, want %v", steps, want)
	}
}

// A listener that fails drains the process, and is what Run returns.
func TestProcess_ListenerFailure(t *testing.T) {
	var logs bytes.Buffer
	p := NewProcess("testapp", 5*time.Second, 0, WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	broken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	broken.Close()
	p.Serve("broken", broken, http.NotFoundHandler())
	var stopped bool
	err = p.Run(func() { stopped = true }, nil)
	if err == nil || !strings.Contains(err.Error(), "testapp broken") {
		t.Fatalf("got %v", err)
	}
	if !stopped || !p.Draining().Load() {
		t.Fatal("a failed listener must drain the process")
	}
	if !strings.Contains(logs.String(), "listener=broken") {
		t.Fatalf("lifecycle not logged: %s", logs.String())
	}
}

// A drain past the deadline is Run's error.
func TestProcess_DrainPastDeadline(t *testing.T) {
	p := NewProcess("testapp", 10*time.Millisecond, 0)
	broken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	broken.Close()
	p.Serve("broken", broken, http.NotFoundHandler())
	stuck := make(chan struct{})
	defer close(stuck)
	if err := p.Run(func() { <-stuck }, nil); err == nil || !strings.Contains(err.Error(), "testapp: shutdown deadline") {
		t.Fatalf("got %v", err)
	}
}

// TestServe runs the single-listener shorthand to completion on a free port:
// the handler is built over the process's draining flag and served, and
// SIGTERM drains it.
func TestServe(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(probe.Addr().String())
	probe.Close()

	var draining *atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- Serve("testapp", port, 5*time.Second, func(d *atomic.Bool) http.Handler {
			draining = d
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "pong") })
		})
	}()

	// Serve binds in its own goroutine; poll until it answers.
	var body []byte
	for range 100 {
		resp, err := http.Get("http://127.0.0.1:" + port + "/")
		if err == nil {
			body, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if string(body) != "pong" {
		t.Fatalf("expected pong, got %q", body)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after SIGTERM")
	}
	if !draining.Load() {
		t.Fatal("the handler's draining flag was not flipped by the drain")
	}
}

func TestServe_PortTaken(t *testing.T) {
	taken, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	_, port, _ := net.SplitHostPort(taken.Addr().String())
	if err := Serve("testapp", port, time.Second, func(*atomic.Bool) http.Handler { return http.NotFoundHandler() }); err == nil {
		t.Fatal("Serve on a taken port must fail")
	}
}
