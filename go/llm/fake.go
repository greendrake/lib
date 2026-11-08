package llm

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// ErrFakeUnscripted is returned by an unscripted Fake — the dev-stack default,
// where every completion errs and the caller abstains.
var ErrFakeUnscripted = errors.New("fake llm: no handler scripted")

// Fake is the deterministic in-process Client for unit tests and the dev
// stack. Tests script it via Handler and assert on the recorded Calls; without
// a handler every call returns ErrFakeUnscripted.
type Fake struct {
	// Handler produces the completion for each request.
	Handler func(Request) (json.RawMessage, error)

	mu    sync.Mutex
	calls []Request
}

func (f *Fake) Complete(_ context.Context, req Request) (Completion, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	if f.Handler == nil {
		return Completion{}, ErrFakeUnscripted
	}
	out, err := f.Handler(req)
	if err != nil {
		return Completion{}, err
	}
	return Completion{JSON: out, Model: string(req.Tier) + "-fake"}, nil
}

// Calls returns a copy of every request received so far.
func (f *Fake) Calls() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Request, len(f.calls))
	copy(out, f.calls)
	return out
}
