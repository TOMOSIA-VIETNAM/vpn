package engine

import (
	"context"
	"time"

	"vpn/internal/routing"
)

// onlinePoll is how often the reconnect wait checks whether the machine has
// a default route. A var so tests can shrink it.
var onlinePoll = time.Second

// hasDefaultRoute reports whether the machine has a default gateway to reach
// the VPN server through. `route get default` looks up the default route
// itself (netmask 0), not the most specific match for 0.0.0.0, so the
// kill switch's 0.0.0.0/1 blackhole does not count as one. A var so tests
// can replace it.
var hasDefaultRoute = func() bool {
	_, err := routing.Capture()
	return err == nil
}

// waitToRetry waits out the reconnect backoff and reports whether to try
// again (false: ctx ended) and whether the network came back meanwhile.
// While waiting it checks for a default route every onlinePoll; once it has
// seen none (Wi-Fi dropped, sleep — at the start or partway through) the
// backoff is cut short the moment one reappears. Before, every attempt
// failed at once with ROUTE_FAILURE and doubled the wait, so the VPN came
// back up to 20 s after the network did. With a network that stays up the
// full backoff applies, so an unreachable server is not hammered.
func waitToRetry(ctx context.Context, wait time.Duration) (retry, cameOnline bool) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	poll := time.NewTicker(onlinePoll)
	defer poll.Stop()
	offline := !hasDefaultRoute()
	for {
		select {
		case <-ctx.Done():
			return false, false
		case <-timer.C:
			return true, false
		case <-poll.C:
			online := hasDefaultRoute()
			if offline && online {
				return true, true
			}
			offline = !online
		}
	}
}
