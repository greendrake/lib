package apiserver

import (
	"net/http"
	"sync/atomic"
	"time"
)

// healthz is the liveness handler: 200 unless draining (then 503).
func healthz(draining *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if draining.Load() {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "draining"})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	}
}

// readyz is the readiness handler. It runs every dependency probe in
// parallel under the given timeout; the response is 200 only when every
// probe succeeds AND the process isn't draining. The body is a map of
// probe-name → "ok" / "fail: <reason>" (plus "draining": true while
// draining) so a failing probe gives operators actionable diagnostics
// rather than a bare 503.
func readyz(checks []DependencyCheck, timeout time.Duration, draining *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		status := http.StatusOK
		if draining.Load() {
			status = http.StatusServiceUnavailable
			body["draining"] = true
		}
		messages, allOK := runChecks(r.Context(), timeout, checks)
		for name, message := range messages {
			body[name] = message
		}
		if !allOK {
			status = http.StatusServiceUnavailable
		}
		WriteJSON(w, status, body)
	}
}

// ProbeMux returns a mux serving GET /healthz and GET /readyz over the
// process's draining flag and its dependency checks; the host mounts its
// own routes on it.
func ProbeMux(draining *atomic.Bool, checks []DependencyCheck, timeout time.Duration) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(draining))
	mux.HandleFunc("GET /readyz", readyz(checks, timeout, draining))
	return mux
}
