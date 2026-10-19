// Package apiserver is generic net/http scaffolding for a service binary:
// dependency health probes with /healthz + /readyz handlers, the process
// lifecycle (listeners, background work, the SIGTERM-driven graceful
// shutdown), CORS middleware, and JSON answers for plain-HTTP endpoints.
package apiserver

import (
	"context"
	"sync"
	"time"
)

// DependencyCheck names a single dependency probe and the function that
// performs it. Check is invoked with a context already bounded by the
// caller's per-dependency timeout — implementations should not add
// their own timeouts on top.
type DependencyCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

// runChecks executes the given checks in parallel under the supplied timeout.
// Each check gets its own derived context with the timeout applied; checks
// that exceed the timeout are reported as failures with a
// context-deadline-exceeded error. messages maps each Check.Name to "ok" or
// "fail: <err>"; allOK is true iff every check passed.
func runChecks(ctx context.Context, timeout time.Duration, checks []DependencyCheck) (messages map[string]string, allOK bool) {
	messages = make(map[string]string, len(checks))
	allOK = true
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range checks {
		wg.Go(func() {
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			message := "ok"
			err := c.Check(cctx)
			if err != nil {
				message = "fail: " + err.Error()
			}
			mu.Lock()
			defer mu.Unlock()
			messages[c.Name] = message
			allOK = allOK && err == nil
		})
	}
	wg.Wait()
	return
}
