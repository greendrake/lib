package camstream

import (
	"context"
	"time"
)

// Monitor is a connected camera stream.
type Monitor interface {
	// Next waits for the stream's next frame.
	Next() (*Frame, error)
	// HasAudio reports whether the camera puts audio in the stream.
	HasAudio() bool
	// ShutDown disconnects, which is also what unblocks a Next waiting on the camera.
	ShutDown()
}

// Open makes one attempt at opening a stream, and may wait until it can make one. ctx
// bounds the attempt alone, and ends early once the source no longer wants the stream; the
// monitor it opens lasts until shut down.
type Open func(ctx context.Context) (Monitor, error)

const (
	// How many frames a subscriber may fall behind before it is dropped: seconds of a
	// stream, which a viewer's socket or a recorder's disk that is merely slow catches up on.
	subscriberBuffer = 256
	// The backoff between failed attempts at opening, from the first doubling to the last.
	firstBackoff = time.Second
	lastBackoff  = 5 * time.Second
)

// Source is one stream fanned out to its subscribers. It opens the stream on its first
// subscriber and closes it after its last. Its one goroutine owns the monitor, so the
// stream is open at most once at a time: a camera cannot tell two monitors of one stream on
// a shared login apart.
type Source struct {
	open Open
	// The backoff's bounds.
	first, last time.Duration
	join        chan *Subscription
	leave       chan *Subscription
	done        chan struct{}
}

// NewSource makes the source of the stream open opens. It runs until ctx ends.
func NewSource(ctx context.Context, open Open) *Source {
	return newSource(ctx, open, firstBackoff, lastBackoff)
}

// newSource makes a source backing off from first, doubling, to last.
func newSource(ctx context.Context, open Open, first, last time.Duration) *Source {
	s := &Source{open: open, first: first, last: last, join: make(chan *Subscription), leave: make(chan *Subscription), done: make(chan struct{})}
	go s.run(ctx)
	return s
}

// Done is closed once the source has stopped with its context, its monitor shut down.
func (s *Source) Done() <-chan struct{} {
	return s.done
}

// Subscription is one subscriber's frames: from the monitor open when it was taken, or
// else the next one to open.
type Subscription struct {
	ctx    context.Context
	frames chan *Frame
	// Stops ending the subscription with its context; set once the source has taken it.
	unwatch func() bool
	// Written before the first frame is sent.
	audio bool
}

// Subscribe takes a subscription until ctx ends. It never waits for the stream.
func (s *Source) Subscribe(ctx context.Context) *Subscription {
	sub := &Subscription{ctx: ctx, frames: make(chan *Frame, subscriberBuffer)}
	select {
	case s.join <- sub:
	case <-s.done:
		close(sub.frames)
	}
	return sub
}

// Frames are the subscription's. The channel closes when the subscription ends: with its
// context, with its monitor, or once it falls a buffer behind.
func (sub *Subscription) Frames() <-chan *Frame {
	return sub.frames
}

// HasAudio reports whether the monitor feeding the subscription carries audio: known once
// the subscription has delivered a frame.
func (sub *Subscription) HasAudio() bool {
	return sub.audio
}

// reading is an open monitor, read by its pump.
type reading struct {
	monitor Monitor
	frames  chan *Frame
	quit    chan struct{}
}

func read(monitor Monitor) *reading {
	r := &reading{monitor: monitor, frames: make(chan *Frame), quit: make(chan struct{})}
	go r.pump()
	return r
}

// pump reads the monitor until it ends, or until quit.
func (r *reading) pump() {
	defer close(r.frames)
	for {
		f, err := r.monitor.Next()
		if err != nil {
			return
		}
		select {
		case r.frames <- f:
		case <-r.quit:
			return
		}
	}
}

// shutDown shuts the monitor down off the caller's goroutine, which a camera slow to
// answer would otherwise hold up: closed once the monitor is shut and its pump gone.
func (r *reading) shutDown() <-chan struct{} {
	shut := make(chan struct{})
	close(r.quit)
	go func() {
		defer close(shut)
		r.monitor.ShutDown()
		for range r.frames {
		}
	}()
	return shut
}

// attempt is an attempt at opening in flight.
type attempt struct {
	cancel context.CancelFunc
	result chan attemptResult
	// Set once the source no longer wants what it comes to.
	abandoned bool
}

type attemptResult struct {
	monitor Monitor
	err     error
}

// run is the source's goroutine: every change of its subscribers and its monitor goes
// through it, and nothing in it waits on the camera.
func (s *Source) run(ctx context.Context) {
	defer close(s.done)
	var (
		subs = map[*Subscription]struct{}{}
		// At most one of these three at a time: the stream opening, open, or the monitor
		// of its last open shutting down, which nothing opens again before.
		opening *attempt
		current *reading
		closing <-chan struct{}
		// Set while backing off after a failed attempt.
		retry   <-chan time.Time
		backoff time.Duration
	)
	end := func(sub *Subscription) {
		delete(subs, sub)
		sub.unwatch()
		close(sub.frames)
	}
	// unwanted gives the stream up once its last subscriber has gone.
	unwanted := func() {
		if len(subs) > 0 {
			return
		}
		switch {
		case current != nil:
			closing = current.shutDown()
			current = nil
		case opening != nil:
			opening.abandoned = true
			opening.cancel()
		}
	}
	for {
		if len(subs) > 0 && opening == nil && current == nil && closing == nil && retry == nil {
			attemptCtx, cancel := context.WithCancel(ctx)
			opening = &attempt{cancel: cancel, result: make(chan attemptResult, 1)}
			go func(result chan<- attemptResult) {
				monitor, err := s.open(attemptCtx)
				result <- attemptResult{monitor, err}
			}(opening.result)
		}
		var (
			opened <-chan attemptResult
			frames <-chan *Frame
		)
		if opening != nil {
			opened = opening.result
		}
		if current != nil {
			frames = current.frames
		}
		select {
		case sub := <-s.join:
			subs[sub] = struct{}{}
			sub.unwatch = context.AfterFunc(sub.ctx, func() {
				select {
				case s.leave <- sub:
				case <-s.done:
				}
			})
			if current != nil {
				sub.audio = current.monitor.HasAudio()
			}
		case sub := <-s.leave:
			if _, live := subs[sub]; live {
				end(sub)
				unwanted()
			}
		case r := <-opened:
			abandoned := opening.abandoned
			opening.cancel()
			opening = nil
			switch {
			case r.err != nil && !abandoned:
				backoff = max(s.first, min(backoff*2, s.last))
				retry = time.After(backoff)
			case r.err != nil:
			case abandoned:
				backoff = 0
				closing = read(r.monitor).shutDown()
			default:
				backoff = 0
				current = read(r.monitor)
				for sub := range subs {
					sub.audio = r.monitor.HasAudio()
				}
			}
		case f, live := <-frames:
			if !live {
				// The monitor ended, and its subscriptions with it: a viewer subscribes
				// again, and a recorder starts a new file.
				closing = current.shutDown()
				current = nil
				for sub := range subs {
					end(sub)
				}
				continue
			}
			for sub := range subs {
				select {
				case sub.frames <- f:
				default:
					end(sub)
				}
			}
			unwanted()
		case <-closing:
			closing = nil
		case <-retry:
			retry = nil
		case <-ctx.Done():
			for sub := range subs {
				end(sub)
			}
			if opening != nil {
				// Its context is the source's, so it is ending too.
				if r := <-opening.result; r.monitor != nil {
					closing = read(r.monitor).shutDown()
				}
			}
			if current != nil {
				closing = current.shutDown()
			}
			if closing != nil {
				<-closing
			}
			return
		}
	}
}
