package engine

import (
	"context"
	"time"

	"vpn/internal/routing"
)

// onlinePoll is how often the reconnect wait checks whether a default route
// is back while the machine has none. A var so tests can shrink it.
var onlinePoll = time.Second

// hasDefaultRoute reports whether the machine has a default gateway to reach
// the VPN server through. A var so tests can replace it.
var hasDefaultRoute = func() bool {
	_, err := routing.Capture()
	return err == nil
}

// waitToRetry waits out the reconnect backoff and reports whether to try
// again (false: ctx ended) and whether the network came back meanwhile.
// When the network is down as the wait starts (no default route: Wi-Fi
// dropped, sleep), the backoff is cut short the moment a default route
// reappears. Before, every attempt failed at once with
// ROUTE_FAILURE and doubled the wait, so the VPN came back up to 20 s after
// the network did. With a working network the full backoff applies, so an
// unreachable server is not hammered.
func waitToRetry(ctx context.Context, wait time.Duration) (retry, cameOnline bool) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	var poll <-chan time.Time
	if !hasDefaultRoute() {
		t := time.NewTicker(onlinePoll)
		defer t.Stop()
		poll = t.C
	}
	for {
		select {
		case <-ctx.Done():
			return false, false
		case <-timer.C:
			return true, false
		case <-poll:
			if hasDefaultRoute() {
				return true, true
			}
		}
	}
}
