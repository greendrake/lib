package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Process is a service binary's serving lifecycle: HTTP listeners bound up
// front and served until SIGINT/SIGTERM, background work the drain waits
// for, and the standard drain over all of it.
type Process struct {
	name      string
	deadline  time.Duration
	lameDuck  time.Duration
	logger    *slog.Logger
	draining  atomic.Bool
	listeners []listener
	wg        sync.WaitGroup
}

type listener struct {
	label string
	ln    net.Listener
	srv   *http.Server
}

// ProcessOption configures NewProcess.
type ProcessOption func(*Process)

// WithLogger is where the process logs its lifecycle; the default is
// slog.Default().
func WithLogger(l *slog.Logger) ProcessOption {
	return func(p *Process) { p.logger = l }
}

// NewProcess returns the lifecycle of the binary name. deadline bounds the
// whole drain; lameDuck is the pause between flipping draining and stopping
// anything (see Run).
func NewProcess(name string, deadline, lameDuck time.Duration, opts ...ProcessOption) *Process {
	p := &Process{name: name, deadline: deadline, lameDuck: lameDuck, logger: slog.Default()}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Draining is the flag the drain flips; the health probes read it.
func (p *Process) Draining() *atomic.Bool {
	return &p.draining
}

// Listen binds port now — a port already taken fails the start rather than
// the serve — and serves h on it once Run starts. label names the listener
// in log lines.
func (p *Process) Listen(label, port string, h http.Handler) error {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("%s listener: %w", label, err)
	}
	p.Serve(label, ln, h)
	return nil
}

// Serve serves h on ln, which the caller has bound, once Run starts: for a binary that
// binds an address of its own choosing, or has to know the port it was given.
func (p *Process) Serve(label string, ln net.Listener, h http.Handler) {
	p.listeners = append(p.listeners, listener{label: label, ln: ln, srv: &http.Server{Handler: h}})
}

// Go runs fn in the background; the drain waits for it to return.
func (p *Process) Go(fn func()) {
	p.wg.Go(fn)
}

// Serve runs a single-listener process to completion: handler — built over
// the process's draining flag, outer middleware (CORS) applied — served on
// port until SIGINT/SIGTERM, then drained under deadline.
func Serve(name, port string, deadline time.Duration, handler func(draining *atomic.Bool) http.Handler) error {
	p := NewProcess(name, deadline, 0)
	if err := p.Listen(name+" API", port, handler(p.Draining())); err != nil {
		return err
	}
	return p.Run(nil, nil)
}

// Run serves every listener until SIGINT or SIGTERM, or until a listener
// fails, then drains under the deadline: stop (the process's non-HTTP work,
// wound down while the probes already answer 503), a graceful shutdown of
// every listener in the order they were added, then release (what outlives
// the listeners — contexts their handlers' background work runs on). stop and
// release may be nil. It returns the listener's failure, or a drain past the
// deadline, which leaves the binary to exit on it.
func (p *Process) Run(stop, release func()) error {
	// Before anything serves: a signal that arrives once a listener answers
	// must drain the process, not kill it.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	failed := make(chan error, len(p.listeners))
	for _, l := range p.listeners {
		p.Go(func() {
			p.logger.Info("listening", "process", p.name, "listener", l.label, "addr", l.ln.Addr().String())
			if err := l.srv.Serve(l.ln); !errors.Is(err, http.ErrServerClosed) {
				failed <- fmt.Errorf("%s %s: %w", p.name, l.label, err)
			}
		})
	}
	p.logger.Info("started", "process", p.name)

	var serveErr error
	select {
	case <-quit:
	case serveErr = <-failed:
	}

	drainErr := shutdown(p.deadline, p.lameDuck, &p.draining, &p.wg, func() {
		if stop != nil {
			stop()
		}
		ctx, cancel := context.WithTimeout(context.Background(), p.deadline)
		defer cancel()
		for _, l := range p.listeners {
			if err := l.srv.Shutdown(ctx); err != nil {
				p.logger.Error("listener shutdown", "process", p.name, "listener", l.label, "err", err)
			}
		}
		if release != nil {
			release()
		}
	})
	if drainErr != nil {
		return errors.Join(serveErr, fmt.Errorf("%s: %w", p.name, drainErr))
	}
	p.logger.Info("stopped", "process", p.name)
	return serveErr
}
