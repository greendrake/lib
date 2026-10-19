package apiserver

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// shutdown executes the SIGTERM-driven graceful-shutdown template. The drain
// *steps* (which servers to stop, in what order) are the process's; the
// *control flow* lives here:
//
//  1. Flip draining → /healthz + /readyz return 503 immediately,
//     orchestrators / monitors observe unreadiness and stop routing
//     new work.
//  2. Sleep lameDuck so the unready state has time to propagate before
//     sockets stop accepting.
//  3. Invoke drain (stop HTTP servers, drain task brokers, cancel contexts,
//     etc., in dependency order).
//  4. Wait on wg so any background goroutines wind down.
//
// The whole sequence races against deadline. If it elapses before drain +
// wg.Wait complete, shutdown returns an error with the drain still running:
// the binary is left to exit on it, and task brokers / HTTP clients are
// designed to recover from that (re-delivery, retry).
func shutdown(deadline, lameDuck time.Duration, draining *atomic.Bool, wg *sync.WaitGroup, drain func()) error {
	draining.Store(true)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if lameDuck > 0 {
			time.Sleep(lameDuck)
		}
		drain()
		wg.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-time.After(deadline):
		return fmt.Errorf("shutdown deadline (%s) exceeded", deadline)
	}
}
