package netwatch

import (
	"context"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func msg(typ byte, flags uint32) []byte {
	m := make([]byte, 16)
	m[offType] = typ
	m[offRtFlags] = byte(flags)
	m[offRtFlags+1] = byte(flags >> 8)
	m[offRtFlags+2] = byte(flags >> 16)
	return m
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		msg  []byte
		want Kind
	}{
		{"route deleted", msg(unix.RTM_DELETE, unix.RTF_UP|unix.RTF_HOST), RouteDeleted},
		{"neighbour cache entry is noise", msg(unix.RTM_DELETE, unix.RTF_LLINFO), 0},
		{"cloned host route is noise", msg(unix.RTM_DELETE, unix.RTF_WASCLONED), 0},
		{"address added", msg(unix.RTM_NEWADDR, 0), LinkChanged},
		{"address removed", msg(unix.RTM_DELADDR, 0), LinkChanged},
		{"link state", msg(unix.RTM_IFINFO, 0), LinkChanged},
		{"route added is not a loss", msg(unix.RTM_ADD, 0), 0},
		{"truncated", []byte{1, 2}, 0},
	}
	for _, c := range cases {
		if got := classify(c.msg); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// Route additions are only reported to SubscribeWithAdds; the data plane's
// feed (classify) keeps ignoring them, as before.
func TestClassifyWithAdds(t *testing.T) {
	cases := []struct {
		name string
		msg  []byte
		want Kind
	}{
		{"route added", msg(unix.RTM_ADD, unix.RTF_UP|unix.RTF_GATEWAY), RouteAdded},
		{"route changed", msg(unix.RTM_CHANGE, unix.RTF_UP|unix.RTF_GATEWAY), RouteAdded},
		{"neighbour cache entry added is noise", msg(unix.RTM_ADD, unix.RTF_LLINFO), 0},
		{"cloned host route added is noise", msg(unix.RTM_ADD, unix.RTF_WASCLONED), 0},
		{"route deleted", msg(unix.RTM_DELETE, unix.RTF_UP), RouteDeleted},
		{"address added", msg(unix.RTM_NEWADDR, 0), LinkChanged},
		{"truncated", []byte{1, 2}, 0},
	}
	for _, c := range cases {
		if got := classifyWithAdds(c.msg); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	if got := classify(msg(unix.RTM_CHANGE, unix.RTF_UP)); got != 0 {
		t.Errorf("classify reported a route change as %v, want it ignored", got)
	}
}

func TestSubscribeClosesOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := Subscribe(ctx)
	if err != nil {
		t.Skip("routing socket unavailable:", err)
	}
	cancel()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("channel not closed after ctx cancel")
		}
	}
}
