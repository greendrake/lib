package apiserver

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Verifies the template's sequencing with a fake drain fn: draining is
// flipped before drain runs, and shutdown returns only after both drain and
// the wg-tracked background goroutine have finished.
func TestShutdown_CleanDrainOrdering(t *testing.T) {
	var draining atomic.Bool
	var wg sync.WaitGroup

	var drainSawDraining bool
	drainDone := make(chan struct{})
	var goroutineFinished atomic.Bool

	wg.Go(func() {
		// Simulates background work that only winds down once drain ran.
		<-drainDone
		goroutineFinished.Store(true)
	})

	err := shutdown(time.Second, 0, &draining, &wg, func() {
		drainSawDraining = draining.Load()
		close(drainDone)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !drainSawDraining {
		t.Fatal("draining must be flipped before drain runs")
	}
	if !goroutineFinished.Load() {
		t.Fatal("shutdown must wait on wg before returning")
	}
}

func TestShutdown_LameDuckDelaysDrain(t *testing.T) {
	var draining atomic.Bool
	var wg sync.WaitGroup

	const lameDuck = 50 * time.Millisecond
	start := time.Now()
	var drainAfter time.Duration
	if err := shutdown(time.Second, lameDuck, &draining, &wg, func() {
		drainAfter = time.Since(start)
	}); err != nil {
		t.Fatal(err)
	}
	if drainAfter < lameDuck {
		t.Fatalf("drain ran %s in, before the %s lame-duck period elapsed", drainAfter, lameDuck)
	}
}

// A drain that outlasts the deadline is left running, and the deadline is
// the error.
func TestShutdown_Deadline(t *testing.T) {
	var draining atomic.Bool
	var wg sync.WaitGroup
	stuck := make(chan struct{})
	defer close(stuck)
	err := shutdown(10*time.Millisecond, 0, &draining, &wg, func() { <-stuck })
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("got %v", err)
	}
}
