// Package netwatch reports network changes as they happen, from the kernel's
// routing socket (the same feed `route monitor` prints), so the engine can
// react to a route being reaped, an address disappearing or a link flapping
// without polling for any of them. An idle machine costs no wakeups at all.
package netwatch

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Kind classifies a routing-socket message down to what the engine acts on.
type Kind int

const (
	// RouteDeleted: a route was removed (ARP/neighbour cache entries, which
	// come and go constantly, are filtered out).
	RouteDeleted Kind = iota + 1
	// LinkChanged: an interface came up/down or an address was added/removed
	// — Wi-Fi roam, cable unplugged, wake from sleep, VPN client elsewhere.
	LinkChanged
	// RouteAdded: a route was added or changed — reported only by
	// SubscribeWithAdds, for waiting on a default route to come back.
	RouteAdded
)

func (k Kind) String() string {
	switch k {
	case RouteDeleted:
		return "route deleted"
	case LinkChanged:
		return "link/address changed"
	case RouteAdded:
		return "route added"
	}
	return "unknown"
}

// rt_msghdr / if_msghdr / ifa_msghdr all begin msglen(2) version(1) type(1);
// rt_msghdr's flags int sits at offset 8 (after index(2) and 2 bytes padding).
const (
	offType    = 3
	offRtFlags = 8
	minMsgLen  = 12
)

// classifyWithAdds is classify plus RouteAdded for added or changed routes
// (the same neighbour-cache / cloned-host churn filtered out).
func classifyWithAdds(msg []byte) Kind {
	if len(msg) >= minMsgLen && (msg[offType] == unix.RTM_ADD || msg[offType] == unix.RTM_CHANGE) {
		if noise(msg) {
			return 0
		}
		return RouteAdded
	}
	return classify(msg)
}

// noise reports a route message about the neighbour cache or a cloned host
// entry: constant churn, never a route the engine cares about.
func noise(msg []byte) bool {
	flags := binary.LittleEndian.Uint32(msg[offRtFlags:]) // macOS runs little-endian on both arm64 and amd64
	return flags&(unix.RTF_LLINFO|unix.RTF_WASCLONED) != 0
}

// classify maps one routing message to an event, or 0 when it is noise.
func classify(msg []byte) Kind {
	if len(msg) < minMsgLen {
		return 0
	}
	switch msg[offType] {
	case unix.RTM_DELETE:
		if noise(msg) {
			return 0
		}
		return RouteDeleted
	case unix.RTM_NEWADDR, unix.RTM_DELADDR, unix.RTM_IFINFO:
		return LinkChanged
	}
	return 0
}

// Subscribe opens the routing socket and delivers events until ctx ends, then
// closes the returned channel. Events are coalesced: if the consumer is busy,
// a burst of the same kind collapses into one pending event, so a flapping
// interface can neither block the reader nor grow memory.
func Subscribe(ctx context.Context) (<-chan Kind, error) {
	return subscribe(ctx, classify)
}

// SubscribeWithAdds is Subscribe that also reports RouteAdded. Not for the
// data plane: the engine adds routes itself, and would see its own changes.
func SubscribeWithAdds(ctx context.Context) (<-chan Kind, error) {
	return subscribe(ctx, classifyWithAdds)
}

func subscribe(ctx context.Context, classify func([]byte) Kind) (<-chan Kind, error) {
	fd, err := unix.Socket(unix.AF_ROUTE, unix.SOCK_RAW, 0)
	if err != nil {
		return nil, fmt.Errorf("open routing socket: %w", err)
	}
	// Non-blocking + os.File so the read goes through Go's network poller:
	// closing the file reliably wakes it (a plain blocking read(2) is not
	// guaranteed to be interrupted by close(2) on macOS) and no OS thread is
	// parked in the kernel while the machine is idle.
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("routing socket non-blocking: %w", err)
	}
	f := os.NewFile(uintptr(fd), "routing-socket")
	out := make(chan Kind, 2)
	stop := context.AfterFunc(ctx, func() { _ = f.Close() })
	go func() {
		defer close(out)
		defer stop()
		defer f.Close()
		buf := make([]byte, 4096)
		for {
			n, err := f.Read(buf)
			if err != nil {
				return // closed by ctx or a dead socket: the consumer's channel just ends
			}
			k := classify(buf[:n])
			if k == 0 {
				continue
			}
			select {
			case out <- k:
			default: // consumer is behind: the queued events already say "something changed"
			}
		}
	}()
	return out, nil
}
