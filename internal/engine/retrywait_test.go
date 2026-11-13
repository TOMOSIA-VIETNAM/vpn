package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func stubDefaultRoute(t *testing.T, online *atomic.Bool) {
	t.Helper()
	prevPoll, prevCheck := onlinePoll, hasDefaultRoute
	onlinePoll = 10 * time.Millisecond
	hasDefaultRoute = online.Load
	t.Cleanup(func() { onlinePoll, hasDefaultRoute = prevPoll, prevCheck })
}

// Wi-Fi drops, then comes back: the reconnect must not sit out the rest of
// a 20 s backoff.
func TestWaitToRetryEndsWhenNetworkReturns(t *testing.T) {
	var online atomic.Bool
	stubDefaultRoute(t, &online)
	time.AfterFunc(50*time.Millisecond, func() { online.Store(true) })
	start := time.Now()
	retry, back := waitToRetry(context.Background(), 20*time.Second)
	if !retry || !back {
		t.Fatalf("retry=%v cameOnline=%v, want both true", retry, back)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("waited %s after the network came back", d)
	}
}

// With a working network the backoff is kept: an unreachable server is not
// retried in a tight loop.
func TestWaitToRetryKeepsBackoffWhileOnline(t *testing.T) {
	var online atomic.Bool
	online.Store(true)
	stubDefaultRoute(t, &online)
	start := time.Now()
	retry, back := waitToRetry(context.Background(), 150*time.Millisecond)
	if !retry || back {
		t.Fatalf("retry=%v cameOnline=%v, want true/false", retry, back)
	}
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Fatalf("returned after %s, before the backoff ended", d)
	}
}

// Wi-Fi drops partway through the wait and comes back: same shortcut.
func TestWaitToRetryEndsWhenNetworkDropsAndReturns(t *testing.T) {
	var online atomic.Bool
	online.Store(true)
	stubDefaultRoute(t, &online)
	time.AfterFunc(40*time.Millisecond, func() { online.Store(false) })
	time.AfterFunc(120*time.Millisecond, func() { online.Store(true) })
	start := time.Now()
	retry, back := waitToRetry(context.Background(), 20*time.Second)
	if !retry || !back {
		t.Fatalf("retry=%v cameOnline=%v, want both true", retry, back)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("waited %s after the network came back", d)
	}
}

func TestWaitToRetryStopsOnDisconnect(t *testing.T) {
	var online atomic.Bool
	stubDefaultRoute(t, &online)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if retry, _ := waitToRetry(ctx, time.Hour); retry {
		t.Fatal("retried after disconnect")
	}
}
