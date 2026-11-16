package slave_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"greendrake.nz/go/transcode"
	"greendrake.nz/go/transcode/internal/transcodetest"
	"greendrake.nz/go/transcode/slave"
)

func TestQueue(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	src := transcodetest.Clip(t, 160, 120)
	first, gate := transcodetest.Gated(transcodetest.Job(src))
	a := transcodetest.Go(context.Background(), s, first)
	<-gate.Reading()
	b := transcodetest.Go(context.Background(), s, transcodetest.Job(src))
	b.Await(t, transcode.Progress{Queued: 0})
	c := transcodetest.Go(context.Background(), s, transcodetest.Job(src))
	c.Await(t, transcode.Progress{Queued: 1})
	if h := s.Health(); h.Running != 1 || h.Waiting != 2 {
		t.Errorf("%d running, %d waiting", h.Running, h.Waiting)
	}
	gate.Open()

	a.Rest(t)
	for name, rest := range map[string][]transcode.Progress{"b": b.Rest(t), "c": c.Rest(t)} {
		if name == "c" {
			if len(rest) == 0 || rest[0] != (transcode.Progress{Queued: 0}) {
				t.Fatalf("c: %+v, no move up the queue", rest)
			}
			rest = rest[1:]
		}
		if len(rest) < 2 || rest[0] != (transcode.Progress{Running: true}) {
			t.Fatalf("%s: %+v", name, rest)
		}
		for _, p := range rest[1:] {
			if !p.Running || p.Queued != 0 {
				t.Errorf("%s: %+v", name, p)
			}
		}
		if last := rest[len(rest)-1]; last.Seconds < 1.9 || last.Seconds > 2.1 {
			t.Errorf("%s: the last progress at %g seconds", name, last.Seconds)
		}
	}
	if h := s.Health(); h.Running != 0 || h.Waiting != 0 {
		t.Errorf("%d running, %d waiting", h.Running, h.Waiting)
	}
}

func TestCancel(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	src := transcodetest.Clip(t, 160, 120)
	first, gate := transcodetest.Gated(transcodetest.Job(src))
	ctx, cancel := context.WithCancel(context.Background())
	a := transcodetest.Go(ctx, s, first)
	<-gate.Reading()
	waiting, stopWaiting := context.WithCancel(context.Background())
	b := transcodetest.Go(waiting, s, transcodetest.Job(src))
	b.Await(t, transcode.Progress{Queued: 0})
	c := transcodetest.Go(context.Background(), s, transcodetest.Job(src))
	c.Await(t, transcode.Progress{Queued: 1})

	stopWaiting()
	if err := <-b.Done; !errors.Is(err, context.Canceled) {
		t.Errorf("b waiting: %v", err)
	}
	c.Await(t, transcode.Progress{Queued: 0})

	cancel()
	// ffmpeg is killed at once; the read waiting at the gate is the caller's, finished before Transcode returns.
	gate.Open()
	if err := <-a.Done; !errors.Is(err, context.Canceled) {
		t.Errorf("a running: %v", err)
	}
	c.Rest(t)
	outcomes := []string{s.Outcome(), s.Outcome(), s.Outcome()}
	if slices.Sort(outcomes); !slices.Equal(outcomes, []string{"cancelled", "cancelled", "ok"}) {
		t.Errorf("logged %q", outcomes)
	}
	if jobs := s.Jobs(t); len(jobs) != 0 {
		t.Errorf("left %v", jobs)
	}
}

func TestClose(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	src := transcodetest.Clip(t, 160, 120)
	first, gate := transcodetest.Gated(transcodetest.Job(src))
	a := transcodetest.Go(context.Background(), s, first)
	<-gate.Reading()
	b := transcodetest.Go(context.Background(), s, transcodetest.Job(src))
	b.Await(t, transcode.Progress{Queued: 0})

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	if err := <-b.Done; !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "closing") {
		t.Errorf("b waiting: %v", err)
	}
	gate.Open()
	if err := <-a.Done; !errors.Is(err, context.Canceled) {
		t.Errorf("a running: %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if mine, err := filepath.Glob(filepath.Join(s.Spool, "*")); err != nil || len(mine) != 0 {
		t.Errorf("the spool holds %v, %v", mine, err)
	}
	if _, err := transcodetest.Run(t, s, src, transcodetest.AVC); !errors.Is(err, context.Canceled) {
		t.Errorf("a job once closed: %v", err)
	}
	for range 3 {
		if outcome := s.Outcome(); outcome != "cancelled" {
			t.Errorf("logged %q", outcome)
		}
	}
}
