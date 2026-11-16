// Package persisttest is what tests of store-backed code build on: Faults,
// the script of what named operations do, which a test's fault-injecting
// store wrappers and hooks run their calls through (Call, Run).
package persisttest

import (
	"sync"
	"testing"
)

// Faults scripts what operations do, by name — a store method as
// "<Store>.<Method>" ("Orders.Insert"), or any name a test's own hooks
// check. An operation takes the next outcome queued for it (nil letting it
// through), else fails with its standing error, if any; a writer set to run
// before it runs once, first — a concurrent change landing mid-operation.
type Faults struct {
	mu     sync.Mutex
	queued map[string][]error
	always map[string]error
	before map[string]func()
}

// NewFaults is a script the test fails on if an outcome queued in it never
// came to pass.
func NewFaults(t testing.TB) *Faults {
	f := &Faults{queued: map[string][]error{}, always: map[string]error{}, before: map[string]func(){}}
	t.Cleanup(func() {
		for op, q := range f.queued {
			if len(q) > 0 {
				t.Errorf("%s: %d scripted outcomes never came to pass", op, len(q))
			}
		}
	})
	return f
}

// Queue has op's next calls take outcomes, in order.
func (f *Faults) Queue(op string, outcomes ...error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queued[op] = append(f.queued[op], outcomes...)
}

// Always has every call of op fail with err; nil lifts it.
func (f *Faults) Always(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.always[op] = err
}

// Before runs writer once, ahead of op's next call.
func (f *Faults) Before(op string, writer func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.before[op] = writer
}

// Check is op's outcome: its before-writer run, then its scripted error or
// nil.
func (f *Faults) Check(op string) error {
	f.mu.Lock()
	writer := f.before[op]
	delete(f.before, op)
	f.mu.Unlock()
	if writer != nil {
		writer()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if q := f.queued[op]; len(q) > 0 {
		f.queued[op] = q[1:]
		return q[0]
	}
	return f.always[op]
}

// Call runs fn unless op's outcome fails it.
func Call[T any](f *Faults, op string, fn func() (T, error)) (T, error) {
	if err := f.Check(op); err != nil {
		var zero T
		return zero, err
	}
	return fn()
}

// Run is Call for an operation answering an error alone.
func Run(f *Faults, op string, fn func() error) error {
	_, err := Call(f, op, func() (struct{}, error) { return struct{}{}, fn() })
	return err
}
