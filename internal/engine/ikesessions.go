package engine

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"vpn/internal/ike"
	"vpn/internal/vpnlog"
)

// ikeSession is what the engine needs from one IKE SA. *ike.Session
// implements it; tests use fakes.
type ikeSession interface {
	SendESP(pkt []byte) error
	RecvESP(ctx context.Context) ([]byte, error)
	SendNATKeepalive() error
	RekeyQuickMode(espProposals []string, localIP, remoteIP net.IP) (*ike.QuickModeResult, error)
	Close()
	Age() time.Duration
	IKELifetime() time.Duration
}

// ikeSessions holds the IKE SAs that currently carry ESP. Normally that is one;
// during a re-authentication it is briefly two — the old one still receiving,
// the new one already sending — so traffic never has to stop for the swap.
type ikeSessions struct {
	ctx context.Context
	in  chan []byte // ESP received on any live session, merged

	mu   sync.RWMutex
	list []ikeSession // oldest first; the last is current
}

func newIKESessions(ctx context.Context) *ikeSessions {
	return &ikeSessions{ctx: ctx, in: make(chan []byte, 512)}
}

// add makes s the session outbound traffic leaves on (and rekeys use), and
// starts feeding its inbound ESP into the merged stream. The session must
// already be reading its socket (StartDataPhase).
func (m *ikeSessions) add(s ikeSession) {
	m.mu.Lock()
	m.list = append(m.list, s)
	m.mu.Unlock()
	go func() {
		for {
			pkt, err := s.RecvESP(m.ctx)
			if err != nil {
				return // session closed/retired, or the connection ended
			}
			select {
			case m.in <- pkt:
			case <-m.ctx.Done():
				return
			}
		}
	}()
}

func (m *ikeSessions) current() ikeSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.list[len(m.list)-1]
}

func (m *ikeSessions) sendESP(pkt []byte) error { return m.current().SendESP(pkt) }

func (m *ikeSessions) recv(ctx context.Context) ([]byte, error) {
	select {
	case pkt := <-m.in:
		return pkt, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// keepalive refreshes the NAT mapping of every live session: the old one still
// receives during the overlap, and its mapping must not lapse under it.
func (m *ikeSessions) keepalive() error {
	m.mu.RLock()
	list := append([]ikeSession(nil), m.list...)
	m.mu.RUnlock()
	var first error
	for _, s := range list {
		if err := s.SendNATKeepalive(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// retire drops s from the set and closes it (which tells the server to delete
// that IKE SA). The last remaining session is never retired: something must
// always be there to send on.
func (m *ikeSessions) retire(s ikeSession) {
	m.mu.Lock()
	for i, x := range m.list {
		if x == s && len(m.list) > 1 {
			m.list = append(m.list[:i], m.list[i+1:]...)
			m.mu.Unlock()
			s.Close()
			return
		}
	}
	m.mu.Unlock()
}

// closeAll closes every session. The list is kept, so a straggler goroutine
// asking for current() during teardown still gets a (closed) session rather
// than a panic.
func (m *ikeSessions) closeAll() {
	m.mu.RLock()
	list := append([]ikeSession(nil), m.list...)
	m.mu.RUnlock()
	for _, s := range list {
		s.Close()
	}
}

// Re-authentication timing. A fresh IKE SA is built once the current one is
// three quarters through its lifetime, so the server never has to delete an
// IKE SA that still carries the tunnel. Vars so tests can shrink them.
var (
	reauthNum, reauthDen = 3, 4
	reauthMinWait        = 5 * time.Second
	reauthRetry          = 2 * time.Minute
	reauthGrace          = 30 * time.Second // old session lingers this long for packets the server still sends on it
)

// reauthDelay is how long to wait before re-authenticating an IKE SA of the
// given lifetime and age; ok=false when the lifetime is unknown.
func reauthDelay(lifetime, age time.Duration) (wait time.Duration, ok bool) {
	if lifetime <= 0 {
		return 0, false
	}
	wait = lifetime*time.Duration(reauthNum)/time.Duration(reauthDen) - age
	if wait < reauthMinWait {
		wait = reauthMinWait
	}
	return wait, true
}

// runReauth keeps the tunnel on a young IKE SA for as long as ctx lives, using
// make-before-break: the new IKE SA and its Quick Mode complete first, outbound
// traffic then moves to them, and only after a grace period is the old IKE SA
// closed. establish builds the new session and its first SA pair. If
// re-authentication keeps failing until the current IKE SA has outlived its
// lifetime, giveUp asks for a full reconnect rather than waiting for the server
// to tear the tunnel down.
func runReauth(ctx context.Context, mux *ikeSessions, sas *saSet, establish func(context.Context) (ikeSession, *ike.QuickModeResult, error), giveUp func(error)) {
	var grace sync.WaitGroup
	defer grace.Wait() // no retirement goroutine outlives the loop
	sleep := func(d time.Duration) bool {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			return true
		}
	}
	for {
		cur := mux.current()
		wait, ok := reauthDelay(cur.IKELifetime(), cur.Age())
		if !ok {
			vpnlog.Info("ENGINE", "the server announced no IKE SA lifetime — no re-authentication scheduled", nil)
			<-ctx.Done()
			return
		}
		if !sleep(wait) {
			return
		}

		next, qm, err := establish(ctx)
		if err == nil {
			err = installReauthed(mux, sas, next, qm)
		}
		if err == nil {
			vpnlog.Info("ENGINE", "IKE SA re-authenticated — now sending on the new one", vpnlog.Fields{
				"in_spi": fmt.Sprintf("%08x", qm.Inbound.SPI), "out_spi": fmt.Sprintf("%08x", qm.Outbound.SPI),
				"ike_lifetime_s": int(next.IKELifetime() / time.Second),
			})
			grace.Add(1)
			go func(old ikeSession) {
				defer grace.Done()
				if sleep(reauthGrace) {
					mux.retire(old)
				}
			}(cur)
			continue
		}

		vpnlog.Error("ENGINE", "IKE re-authentication failed", vpnlog.Fields{"err": err, "ike_age_s": int(cur.Age() / time.Second)})
		if l := cur.IKELifetime(); l > 0 && cur.Age() > l {
			giveUp(fmt.Errorf("IKE SA outlived its %s lifetime and re-authentication keeps failing: %w", l.Round(time.Second), err))
			return
		}
		if !sleep(reauthRetry) {
			return
		}
	}
}

// installReauthed switches outbound traffic to the new session and SA pair.
// The session goes first: until the pair is installed the old pair is still
// what encrypts, which the server accepts on either socket.
func installReauthed(mux *ikeSessions, sas *saSet, next ikeSession, qm *ike.QuickModeResult) error {
	mux.add(next)
	if err := sas.install(qm, time.Now()); err != nil {
		mux.retire(next) // back to the old session as current
		return fmt.Errorf("install SA pair from the new IKE SA: %w", err)
	}
	return nil
}
