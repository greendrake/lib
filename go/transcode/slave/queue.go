package slave

import (
	"context"
	"slices"
	"sync"

	"greendrake.nz/go/transcode"
)

// queue hands the slave's slots to its jobs, in arrival order.
type queue struct {
	mu      sync.Mutex
	free    int // slots no job holds
	running int
	waiting []*waiter // in arrival order
}

// waiter is a job waiting for a slot.
type waiter struct {
	granted chan struct{} // closed once the job has its slot
	place   chan int      // the jobs ahead of it, as they change: the latest only
}

// wait has a job wait for a slot, telling report its place as it changes, and answers the slot's release; or the
// cause of ctx's end, which ends the wait.
func (q *queue) wait(ctx context.Context, report func(transcode.Status)) (func(), error) {
	q.mu.Lock()
	if q.free > 0 {
		q.free--
		q.running++
		q.mu.Unlock()
		return q.release, nil
	}
	w := &waiter{granted: make(chan struct{}), place: make(chan int, 1)}
	q.waiting = append(q.waiting, w)
	q.tell(len(q.waiting) - 1)
	q.mu.Unlock()
	for {
		select {
		case <-w.granted:
			return q.release, nil
		case ahead := <-w.place:
			report(transcode.Queued{Ahead: ahead})
		case <-ctx.Done():
			q.mu.Lock()
			i := slices.Index(q.waiting, w)
			if i >= 0 {
				q.waiting = slices.Delete(q.waiting, i, i+1)
				q.tell(i)
			}
			q.mu.Unlock()
			if i < 0 { // granted as ctx ended
				q.release()
			}
			return nil, context.Cause(ctx)
		}
	}
}

// release hands a job's slot to the first job waiting, if any.
func (q *queue) release() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.waiting) == 0 {
		q.free++
		q.running--
		return
	}
	close(q.waiting[0].granted)
	q.waiting = slices.Delete(q.waiting, 0, 1)
	q.tell(0)
}

// tell has each job waiting from the from-th on learn its place, which has changed. The caller holds q.mu, so
// nothing but the waiter takes from its place channel meanwhile, and the send after emptying it never blocks.
func (q *queue) tell(from int) {
	for i, w := range q.waiting[from:] {
		select {
		case <-w.place:
		default:
		}
		w.place <- from + i
	}
}

// counts are the jobs running and waiting.
func (q *queue) counts() (running, waiting int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.running, len(q.waiting)
}
