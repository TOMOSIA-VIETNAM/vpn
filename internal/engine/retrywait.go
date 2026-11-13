package engine

import (
	"context"
	"time"

	"vpn/internal/netwatch"
	"vpn/internal/routing"
	"vpn/internal/vpnlog"
)

// netSettle lets a burst of network events (DHCP adds an address, then the
// default route, then host routes) finish before the default route is
// checked, so one burst costs one check. A var so tests can shrink it.
var netSettle = 300 * time.Millisecond

// onlinePoll is how often the reconnect wait checks for a default route when
// the kernel's network event feed is unavailable. A var so tests can shrink it.
var onlinePoll = time.Second

// subscribeNet delivers the kernel's routing-socket events, route additions
// included. A var so tests can feed events.
var subscribeNet = netwatch.SubscribeWithAdds

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
//
// Once it has seen no default route (Wi-Fi dropped, sleep — at the start or
// partway through) the backoff is cut short the moment one reappears.
// Before, every attempt failed at once with ROUTE_FAILURE and doubled the
// wait, so the VPN came back up to 20 s after the network did. With a
// network that stays up the full backoff applies, so an unreachable server
// is not hammered.
//
// The default route is checked only when the kernel reports a network change
// (routing socket, see netwatch): a quiet wait runs no command at all. If the
// event feed cannot be opened or ends, it falls back to checking every
// onlinePoll.
func waitToRetry(ctx context.Context, wait time.Duration) (retry, cameOnline bool) {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var poll <-chan time.Time
	startPolling := func(reason string, err error) {
		vpnlog.Info("ENGINE", "network event feed unavailable while waiting to reconnect — checking the default route every "+onlinePoll.String(), vpnlog.Fields{"reason": reason, "err": err})
		t := time.NewTicker(onlinePoll)
		poll = t.C
		context.AfterFunc(subCtx, t.Stop)
	}
	events, err := subscribeNet(subCtx)
	if err != nil {
		startPolling("subscribe failed", err)
	}

	offline := !hasDefaultRoute()
	var settle <-chan time.Time
	check := func() bool {
		online := hasDefaultRoute()
		back := offline && online
		offline = !online
		return back
	}
	for {
		select {
		case <-ctx.Done():
			return false, false
		case <-timer.C:
			return true, false
		case _, ok := <-events:
			if !ok {
				events = nil
				startPolling("feed ended", nil)
				continue
			}
			if settle == nil {
				settle = time.After(netSettle)
			}
		case <-settle:
			settle = nil
			if check() {
				return true, true
			}
		case <-poll:
			if check() {
				return true, true
			}
		}
	}
}
