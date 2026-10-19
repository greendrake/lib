package camstream_test

import (
	"context"
	"testing"
	"time"

	"greendrake.nz/go/camstream"
	"greendrake.nz/go/camstream/camstreamtest"
)

// source runs over cam until the test ends, backing off from 25 ms to 100 ms.
func source(t *testing.T, cam *camstreamtest.Camera) *camstream.Source {
	ctx, cancel := context.WithCancel(context.Background())
	s := camstream.NewSourceBackingOff(ctx, cam.Open, 25*time.Millisecond, 100*time.Millisecond)
	t.Cleanup(func() {
		cancel()
		<-s.Done()
	})
	return s
}

// subscribe takes a subscription until the test ends or cancel.
func subscribe(t *testing.T, s *camstream.Source) (*camstream.Subscription, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return s.Subscribe(ctx), cancel
}

func within(t *testing.T, what string, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func receiveFrame(t *testing.T, sub *camstream.Subscription) *camstream.Frame {
	t.Helper()
	select {
	case f, live := <-sub.Frames():
		if !live {
			t.Fatal("the subscription ended")
		}
		return f
	case <-time.After(2 * time.Second):
		t.Fatal("no frame came")
	}
	return nil
}

// ends is closed once sub has ended, frames left unread.
func ends(sub *camstream.Subscription) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		for range sub.Frames() {
		}
		close(done)
	}()
	return done
}

func TestSourceFeedsEverySubscriber(t *testing.T) {
	cam := camstreamtest.NewCamera(t)
	s := source(t, cam)
	first, _ := subscribe(t, s)
	monitor := camstreamtest.NewMonitor()
	monitor.Audio = true
	cam.Opens(monitor)
	second, _ := subscribe(t, s)
	sent := &camstream.Frame{Kind: camstream.Video, Keyframe: true}
	monitor.Frames <- sent
	for _, sub := range []*camstream.Subscription{first, second} {
		if f := receiveFrame(t, sub); f != sent || !sub.HasAudio() {
			t.Fatalf("got %+v, audio %v", f, sub.HasAudio())
		}
	}
}

// The stream opens on the first subscriber, and closes after the last.
func TestStreamIsOpenWhileSubscribed(t *testing.T) {
	cam := camstreamtest.NewCamera(t)
	s := source(t, cam)
	_, leaveFirst := subscribe(t, s)
	monitor := camstreamtest.NewMonitor()
	cam.Opens(monitor)
	second, leaveSecond := subscribe(t, s)
	leaveFirst()
	monitor.Frames <- &camstream.Frame{Kind: camstream.Video}
	receiveFrame(t, second)
	leaveSecond()
	within(t, "the monitor to be shut down", monitor.Shut)
	within(t, "the subscription to end", ends(second))
	subscribe(t, s)
	cam.Opens(camstreamtest.NewMonitor())
}

// The stream opens again only once its last monitor is shut: on a shared login the camera
// could not tell the two apart.
func TestOpenWaitsForTheLastMonitor(t *testing.T) {
	opened := make(chan struct{}, 1)
	cam := camstreamtest.NewCamera(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := camstream.NewSource(ctx, func(ctx context.Context) (camstream.Monitor, error) {
		opened <- struct{}{}
		return cam.Open(ctx)
	})
	_, leave := subscribe(t, s)
	<-opened
	stopping := camstreamtest.NewMonitor()
	stopping.Release = make(chan struct{})
	cam.Opens(stopping)
	leave()
	within(t, "the monitor to be shutting down", stopping.Shutting)
	subscribe(t, s)
	select {
	case <-opened:
		t.Fatal("the stream opened while its last monitor was shutting down")
	case <-time.After(50 * time.Millisecond):
	}
	close(stopping.Release)
	within(t, "the stream to open again", opened)
	cam.Opens(camstreamtest.NewMonitor())
}

// A failed open leaves its subscriptions waiting, and is tried again, backing off; an open
// that succeeds resets the backoff.
func TestFailedOpensBackOff(t *testing.T) {
	cam := camstreamtest.NewCamera(t)
	s := source(t, cam)
	sub, _ := subscribe(t, s)
	for range 4 {
		cam.Opens(nil)
	}
	monitor := camstreamtest.NewMonitor()
	cam.Opens(monitor)
	monitor.Frames <- &camstream.Frame{Kind: camstream.Video}
	receiveFrame(t, sub)
	monitor.End()
	within(t, "the subscription to end with its monitor", ends(sub))
	subscribe(t, s)
	cam.Opens(nil)
	cam.Opens(camstreamtest.NewMonitor())
	attempts := cam.Attempts()
	var gaps []time.Duration
	for i := 1; i < len(attempts); i++ {
		gaps = append(gaps, attempts[i].Sub(attempts[i-1]))
	}
	// Each after the backoff it waited out: 25, 50, 100, 100; none past the success; and
	// 25 again, rather than the 100 it had got to.
	for i, least := range []time.Duration{25, 50, 100, 100, 0, 25} {
		if gaps[i] < least*time.Millisecond {
			t.Fatalf("attempts %v apart", gaps)
		}
	}
	if gaps[5] >= 75*time.Millisecond {
		t.Fatalf("the backoff did not reset: attempts %v apart", gaps)
	}
}

// A monitor that ends ends its subscriptions, and is shut down.
func TestSubscriptionsEndWithTheirMonitor(t *testing.T) {
	cam := camstreamtest.NewCamera(t)
	s := source(t, cam)
	sub, _ := subscribe(t, s)
	monitor := camstreamtest.NewMonitor()
	cam.Opens(monitor)
	monitor.End()
	within(t, "the subscription to end", ends(sub))
	within(t, "the monitor to be shut down", monitor.Shut)
}

// A subscriber that falls a buffer behind is dropped, and the others go on.
func TestSlowSubscriberIsDropped(t *testing.T) {
	cam := camstreamtest.NewCamera(t)
	s := source(t, cam)
	slow, _ := subscribe(t, s)
	monitor := camstreamtest.NewMonitor()
	cam.Opens(monitor)
	fast, _ := subscribe(t, s)
	for range camstream.SubscriberBuffer + 1 {
		monitor.Frames <- &camstream.Frame{Kind: camstream.Video}
		receiveFrame(t, fast)
	}
	within(t, "the slow subscriber to be dropped", ends(slow))
	monitor.Frames <- &camstream.Frame{Kind: camstream.Video}
	receiveFrame(t, fast)
}

// An open in flight when the last subscriber leaves is cancelled, and a monitor it comes
// to regardless is shut down.
func TestLeavingAbandonsAnOpenInFlight(t *testing.T) {
	late := camstreamtest.NewMonitor()
	inFlight := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := camstream.NewSource(ctx, func(ctx context.Context) (camstream.Monitor, error) {
		close(inFlight)
		<-ctx.Done()
		return late, nil
	})
	_, leave := subscribe(t, s)
	within(t, "the open to be in flight", inFlight)
	leave()
	within(t, "the late monitor to be shut down", late.Shut)
}

func TestSourceStopsWithItsContext(t *testing.T) {
	cam := camstreamtest.NewCamera(t)
	ctx, cancel := context.WithCancel(context.Background())
	s := camstream.NewSourceBackingOff(ctx, cam.Open, time.Millisecond, time.Millisecond)
	sub, _ := subscribe(t, s)
	monitor := camstreamtest.NewMonitor()
	cam.Opens(monitor)
	cancel()
	within(t, "the source to stop", s.Done())
	within(t, "the subscription to end", ends(sub))
	within(t, "the monitor to be shut down", monitor.Shut)
	if _, live := <-s.Subscribe(context.Background()).Frames(); live {
		t.Fatal("a stopped source took a subscription")
	}
}
