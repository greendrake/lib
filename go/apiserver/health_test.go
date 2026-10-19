package apiserver

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRunChecks_Empty(t *testing.T) {
	messages, allOK := runChecks(context.Background(), time.Second, nil)
	if !allOK {
		t.Fatal("no checks must report allOK")
	}
	if len(messages) != 0 {
		t.Fatalf("expected no messages, got %v", messages)
	}
}

func TestRunChecks_MixedOutcomes(t *testing.T) {
	checks := []DependencyCheck{
		{Name: "good", Check: func(context.Context) error { return nil }},
		{Name: "bad", Check: func(context.Context) error { return errors.New("boom") }},
	}
	messages, allOK := runChecks(context.Background(), time.Second, checks)
	if allOK {
		t.Fatal("a failing check must clear allOK")
	}
	if messages["good"] != "ok" || messages["bad"] != "fail: boom" {
		t.Fatalf("messages: %v", messages)
	}
}

func TestRunChecks_TimeoutBoundsSlowCheck(t *testing.T) {
	checks := []DependencyCheck{
		// Blocks until its context is cancelled — the per-check timeout must
		// be what unblocks it and turn into a reported failure.
		{Name: "slow", Check: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}},
	}
	messages, allOK := runChecks(context.Background(), 10*time.Millisecond, checks)
	if allOK {
		t.Fatal("a timed-out check must clear allOK")
	}
	if messages["slow"] != "fail: "+context.DeadlineExceeded.Error() {
		t.Fatalf("slow check: %v", messages)
	}
}

func TestRunChecks_RunsInParallel(t *testing.T) {
	// Rendezvous: each check waits for the other to have started. If checks
	// ran sequentially, neither would ever release the other and both would
	// fail on the timeout instead.
	var barrier sync.WaitGroup
	barrier.Add(2)
	rendezvous := func(ctx context.Context) error {
		barrier.Done()
		done := make(chan struct{})
		go func() { barrier.Wait(); close(done) }()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	checks := []DependencyCheck{
		{Name: "a", Check: rendezvous},
		{Name: "b", Check: rendezvous},
	}
	_, allOK := runChecks(context.Background(), time.Second, checks)
	if !allOK {
		t.Fatal("checks did not run in parallel: rendezvous timed out")
	}
}
