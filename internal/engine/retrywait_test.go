package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"vpn/internal/netwatch"
)

// fakeNet stands in for the default route and the kernel's event feed.
type fakeNet struct {
	online atomic.Bool
	checks atomic.Int32 // hasDefaultRoute calls
	events chan netwatch.Kind
}

// stubNet installs a fake network. subErr makes subscribing fail.
func stubNet(t *testing.T, online bool, subErr error) *fakeNet {
	t.Helper()
	n := &fakeNet{events: make(chan netwatch.Kind, 8)}
	n.online.Store(online)
	prevPoll, prevSettle, prevCheck, prevSub := onlinePoll, netSettle, hasDefaultRoute, subscribeNet
	onlinePoll = 10 * time.Millisecond
	netSettle = 5 * time.Millisecond
	hasDefaultRoute = func() bool { n.checks.Add(1); return n.online.Load() }
	subscribeNet = func(context.Context) (<-chan netwatch.Kind, error) {
		if subErr != nil {
			return nil, subErr
		}
		return n.events, nil
	}
	t.Cleanup(func() {
		onlinePoll, netSettle, hasDefaultRoute, subscribeNet = prevPoll, prevSettle, prevCheck, prevSub
	})
	return n
}

func (n *fakeNet) after(d time.Duration, online bool, k netwatch.Kind) {
	time.AfterFunc(d, func() { n.online.Store(online); n.events <- k })
}

func wantBackSoon(t *testing.T, start time.Time, retry, back bool) {
	t.Helper()
	if !retry || !back {
		t.Fatalf("retry=%v cameOnline=%v, want both true", retry, back)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("waited %s after the network came back", d)
	}
}

// Wi-Fi drops, then comes back: the reconnect must not sit out the rest of
// a 20 s backoff.
func TestWaitToRetryEndsWhenNetworkReturns(t *testing.T) {
	n := stubNet(t, false, nil)
	n.after(30*time.Millisecond, true, netwatch.LinkChanged)
	n.after(40*time.Millisecond, true, netwatch.RouteAdded) // same burst
	start := time.Now()
	retry, back := waitToRetry(context.Background(), 20*time.Second)
	wantBackSoon(t, start, retry, back)
	// One check at the start, one after the burst settled: no polling.
	if c := n.checks.Load(); c != 2 {
		t.Fatalf("checked the default route %d times, want 2 (start + one per settled burst)", c)
	}
}

// Wi-Fi drops partway through the wait and comes back: same shortcut.
func TestWaitToRetryEndsWhenNetworkDropsAndReturns(t *testing.T) {
	n := stubNet(t, true, nil)
	n.after(30*time.Millisecond, false, netwatch.RouteDeleted)
	n.after(100*time.Millisecond, true, netwatch.RouteAdded)
	start := time.Now()
	retry, back := waitToRetry(context.Background(), 20*time.Second)
	wantBackSoon(t, start, retry, back)
}

// With a working network the backoff is kept — even through network events
// that leave the default route in place — so an unreachable server is not
// retried in a tight loop. A quiet wait checks only once.
func TestWaitToRetryKeepsBackoffWhileOnline(t *testing.T) {
	n := stubNet(t, true, nil)
	n.after(20*time.Millisecond, true, netwatch.LinkChanged)
	start := time.Now()
	retry, back := waitToRetry(context.Background(), 150*time.Millisecond)
	if !retry || back {
		t.Fatalf("retry=%v cameOnline=%v, want true/false", retry, back)
	}
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Fatalf("returned after %s, before the backoff ended", d)
	}
	if c := n.checks.Load(); c != 2 {
		t.Fatalf("checked the default route %d times, want 2 (start + the one event)", c)
	}
}

// Offline with no network events: nothing is polled, the full backoff runs.
func TestWaitToRetryQuietWaitRunsNoChecks(t *testing.T) {
	n := stubNet(t, false, nil)
	retry, back := waitToRetry(context.Background(), 100*time.Millisecond)
	if !retry || back {
		t.Fatalf("retry=%v cameOnline=%v, want true/false", retry, back)
	}
	if c := n.checks.Load(); c != 1 {
		t.Fatalf("checked the default route %d times during a quiet wait, want 1", c)
	}
}

// No routing socket: fall back to polling, and still catch the return.
func TestWaitToRetryPollsWhenSubscribeFails(t *testing.T) {
	n := stubNet(t, false, errors.New("open routing socket: permission denied"))
	time.AfterFunc(50*time.Millisecond, func() { n.online.Store(true) })
	start := time.Now()
	retry, back := waitToRetry(context.Background(), 20*time.Second)
	wantBackSoon(t, start, retry, back)
}

// The event feed ends mid-wait: fall back to polling.
func TestWaitToRetryPollsWhenFeedEnds(t *testing.T) {
	n := stubNet(t, false, nil)
	time.AfterFunc(20*time.Millisecond, func() { close(n.events) })
	time.AfterFunc(60*time.Millisecond, func() { n.online.Store(true) })
	start := time.Now()
	retry, back := waitToRetry(context.Background(), 20*time.Second)
	wantBackSoon(t, start, retry, back)
}

func TestWaitToRetryStopsOnDisconnect(t *testing.T) {
	stubNet(t, false, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if retry, _ := waitToRetry(ctx, time.Hour); retry {
		t.Fatal("retried after disconnect")
	}
}

// The real feed is closed when the wait ends, whichever way it ends.
func TestWaitToRetryClosesFeed(t *testing.T) {
	stubNet(t, true, nil)
	var subCtx context.Context
	subscribeNet = func(ctx context.Context) (<-chan netwatch.Kind, error) {
		subCtx = ctx
		return make(chan netwatch.Kind), nil
	}
	waitToRetry(context.Background(), 10*time.Millisecond)
	select {
	case <-subCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("event feed still open after the wait returned")
	}
}
