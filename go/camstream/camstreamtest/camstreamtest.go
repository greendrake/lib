// Package camstreamtest is camera streams in memory, for the tests of what takes
// camstream's monitors and sources: a monitor sent its frames, a camera that opens the
// monitors handed to it, a camera's recorded stream, and H.265 fixtures.
package camstreamtest

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"greendrake.nz/go/camstream"
)

// Monitor is a camstream.Monitor yielding the frames sent to it until it ends.
type Monitor struct {
	// What Next yields, as it is sent.
	Frames chan *camstream.Frame
	// What HasAudio reports.
	Audio bool
	// Closed as ShutDown is called, and once it has shut the monitor.
	Shutting, Shut chan struct{}
	// Blocks ShutDown until closed, where a test sets it.
	Release chan struct{}

	ended    chan struct{}
	endOnce  sync.Once
	shutOnce sync.Once
}

func NewMonitor() *Monitor {
	return &Monitor{Frames: make(chan *camstream.Frame), Shutting: make(chan struct{}), Shut: make(chan struct{}), ended: make(chan struct{})}
}

func (m *Monitor) Next() (*camstream.Frame, error) {
	select {
	case f := <-m.Frames:
		return f, nil
	case <-m.ended:
		return nil, errors.New("camstreamtest: the stream ended")
	}
}

func (m *Monitor) HasAudio() bool {
	return m.Audio
}

func (m *Monitor) ShutDown() {
	m.shutOnce.Do(func() {
		close(m.Shutting)
		if m.Release != nil {
			<-m.Release
		}
		m.End()
		close(m.Shut)
	})
}

// End is the camera ending the stream.
func (m *Monitor) End() {
	m.endOnce.Do(func() { close(m.ended) })
}

// Camera is a stream's camera: each attempt at opening it waits for the monitor Opens
// hands it, until its context ends.
type Camera struct {
	t        testing.TB
	monitors chan *Monitor
	mu       sync.Mutex
	attempts []time.Time
}

func NewCamera(t testing.TB) *Camera {
	return &Camera{t: t, monitors: make(chan *Monitor)}
}

// Open is a camstream.Open: it opens the monitor it is handed, and fails where it is
// handed nil.
func (c *Camera) Open(ctx context.Context) (camstream.Monitor, error) {
	c.mu.Lock()
	c.attempts = append(c.attempts, time.Now())
	c.mu.Unlock()
	select {
	case m := <-c.monitors:
		if m == nil {
			return nil, errors.New("camstreamtest: the camera is away")
		}
		return m, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Opens has the next attempt open m, nil failing it, and fails the test where no attempt
// comes.
func (c *Camera) Opens(m *Monitor) {
	c.t.Helper()
	select {
	case c.monitors <- m:
	case <-time.After(5 * time.Second):
		c.t.Fatal("camstreamtest: no attempt at opening the stream")
	}
}

// Attempts are when each attempt at opening was made, in order.
func (c *Camera) Attempts() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.attempts)
}
