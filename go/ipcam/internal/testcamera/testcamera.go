// Package testcamera runs the engine on cameras for tests, a camera in memory or a fake
// one through a protocol's adapter: a camera's connection, or a fleet of them, with what
// they push.
package testcamera

import (
	"context"
	"testing"
	"time"

	"greendrake.nz/go/ipcam"
	"greendrake.nz/go/ipcam/connection"
	"greendrake.nz/go/ipcam/fleet"
)

// Pushes are what was pushed, in order.
type Pushes[T any] struct {
	t      *testing.T
	pushed chan T
}

func newPushes[T any](t *testing.T) *Pushes[T] {
	return &Pushes[T]{t: t, pushed: make(chan T, 64)}
}

func (p *Pushes[T]) push(v T) {
	p.pushed <- v
}

// Await waits for a push that satisfies want, passing over those before it.
func (p *Pushes[T]) Await(what string, want func(T) bool) T {
	p.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case v := <-p.pushed:
			if want(v) {
				return v
			}
		case <-deadline:
			p.t.Fatalf("no push %s", what)
		}
	}
}

// Harness is a camera's connection, run for a test.
type Harness struct {
	*Pushes[connection.State]
	Conn *connection.Connection
}

// Conn is the connection to the camera at address through driver, dialled, for whatever
// runs it.
func Conn(t *testing.T, driver ipcam.Driver, address string) *connection.Connection {
	t.Helper()
	conn, err := connection.Dial(context.Background(), driver, address, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

// Dial connects to the camera at address through driver, and keeps it connected for the
// test.
func Dial(t *testing.T, driver ipcam.Driver, address string) *Harness {
	t.Helper()
	return run(t, Conn(t, driver, address))
}

// Reboot reboots the camera conn is online on.
func Reboot(t *testing.T, conn *connection.Connection) {
	t.Helper()
	settings, err := conn.Configurable()
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Reboot(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Start starts the connection to the camera at address through driver, offline until it
// has dialled it, and runs it for the test.
func Start(t *testing.T, driver ipcam.Driver, address string) *Harness {
	return run(t, connection.Start(driver, address, "admin", ""))
}

func run(t *testing.T, conn *connection.Connection) *Harness {
	h := &Harness{Pushes: newPushes[connection.State](t), Conn: conn}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn.Run(ctx, h.push)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return h
}

// Pushed is a camera's state, as a fleet pushes it.
type Pushed struct {
	Name  string
	State connection.State
}

// Fleet is a fleet, run for a test.
type Fleet struct {
	*Pushes[Pushed]
	*fleet.Fleet
}

// RunFleet runs cameras for the test, recording to recordDir, each push recorded, until
// the test ends.
func RunFleet(t *testing.T, cameras map[string]fleet.Camera, recordDir string, opts ...fleet.Option) *Fleet {
	ctx, cancel := context.WithCancel(context.Background())
	f := &Fleet{Pushes: newPushes[Pushed](t), Fleet: fleet.New(ctx, cameras, recordDir, opts...)}
	t.Cleanup(func() {
		cancel()
		f.Wait()
	})
	f.Start(func(name string, state connection.State) { f.push(Pushed{Name: name, State: state}) })
	return f
}

// Online is a state the connection is online in.
func Online(s connection.State) bool { return s.State == connection.Online }

// Offline is a state the connection is offline in.
func Offline(s connection.State) bool { return s.State == connection.Offline }

// Of is a push of the named camera in a state want takes.
func Of(name string, want func(connection.State) bool) func(Pushed) bool {
	return func(p Pushed) bool { return p.Name == name && want(p.State) }
}
