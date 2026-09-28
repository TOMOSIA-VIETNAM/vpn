package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"vpn/internal/ike"
	"vpn/internal/ipsec"
	"vpn/internal/vpnlog"
)

// saPair is one negotiated ESP SA pair and when it stops being valid.
type saPair struct {
	in, out  *ipsec.SA
	expires  time.Time
	lifetime time.Duration
}

// saSet holds every ESP SA pair the tunnel may still receive on. Outbound
// traffic always uses the newest pair; inbound is matched by the SPI on
// each packet, so packets the server still sends on the previous pair
// right after a rekey are not lost. L2TP and PPP sit above this and never
// notice a rekey — no re-authentication, no session interruption.
type saSet struct {
	mu    sync.RWMutex
	pairs []saPair // oldest first; the last one is current

	// kick asks runRekey to rekey right now — sent when the server deletes
	// the pair in use, so the tunnel does not wait half a lifetime to recover.
	kick chan struct{}

	// changed is signalled whenever a pair is installed, so the rekey schedule
	// follows the newest pair — notably one the *server* rekeyed for us.
	changed chan struct{}
}

func newSASet(qm *ike.QuickModeResult) (*saSet, error) {
	s := &saSet{kick: make(chan struct{}, 1), changed: make(chan struct{}, 1)}
	if err := s.install(qm, time.Now()); err != nil {
		return nil, err
	}
	return s, nil
}

// install makes qm's pair current. Older pairs stay for inbound until the
// server deletes them or they expire.
func (s *saSet) install(qm *ike.QuickModeResult, now time.Time) error {
	out, err := newESPSA(qm.Outbound)
	if err != nil {
		return fmt.Errorf("outbound ESP SA: %w", err)
	}
	in, err := newESPSA(qm.Inbound)
	if err != nil {
		return fmt.Errorf("inbound ESP SA: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pairs = append(s.pairs, saPair{in: in, out: out, expires: now.Add(qm.Lifetime), lifetime: qm.Lifetime})
	select {
	case s.changed <- struct{}{}:
	default:
	}
	return nil
}

func (s *saSet) current() saPair {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pairs[len(s.pairs)-1]
}

// inbound returns the SA a packet with this SPI belongs to, or nil.
func (s *saSet) inbound(spi uint32) *ipsec.SA {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.pairs) - 1; i >= 0; i-- {
		if s.pairs[i].in.SPI == spi {
			return s.pairs[i].in
		}
	}
	return nil
}

// drop removes the non-current pairs the peer deleted. A Delete names
// either direction's SPI depending on the implementation, so both are
// matched. The current pair is kept even if named: without it nothing can
// be sent at all, and the next rekey replaces it anyway.
func (s *saSet) drop(spis []uint32) (droppedCurrent bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	named := func(p saPair) bool {
		for _, spi := range spis {
			if p.in.SPI == spi || p.out.SPI == spi {
				return true
			}
		}
		return false
	}
	kept := s.pairs[:0]
	last := len(s.pairs) - 1
	for i, p := range s.pairs {
		if named(p) {
			if i == last {
				droppedCurrent = true
			} else {
				p.in.Zeroize()
				p.out.Zeroize()
				continue
			}
		}
		kept = append(kept, p)
	}
	s.pairs = kept
	if droppedCurrent {
		select {
		case s.kick <- struct{}{}:
		default:
		}
	}
	return droppedCurrent
}

// expireOld removes non-current pairs past their lifetime.
func (s *saSet) expireOld(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.pairs[:0]
	last := len(s.pairs) - 1
	for i, p := range s.pairs {
		if i == last || now.Before(p.expires) {
			kept = append(kept, p)
		} else {
			p.in.Zeroize()
			p.out.Zeroize()
		}
	}
	s.pairs = kept
}

// Rekey timing. The client replaces the pair once half its lifetime has
// passed: with strongSwan's defaults (margintime 9m, rekeyfuzz 100%) the
// server would start its own rekey no earlier than lifetime-18m, and this
// client cannot answer a server-initiated Quick Mode — so it must always
// get there first. Failures retry well before the pair actually expires.
const (
	rekeyFraction   = 2 // rekey at lifetime/rekeyFraction
	minRekeyWait    = 30 * time.Second
	rekeyRetryEvery = 30 * time.Second
)

// runRekey keeps the tunnel's SA pair fresh for as long as ctx lives.
// override > 0 replaces the lifetime-based schedule (for testing a rekey
// without waiting half an hour).
func runRekey(ctx context.Context, sas *saSet, session func() ikeSession, rekey func() (*ike.QuickModeResult, error), lifetime, override time.Duration, giveUp func(error)) {
	schedule := func(l time.Duration) time.Duration {
		if override > 0 {
			return override
		}
		return rekeyDelay(l)
	}
	next := schedule(lifetime)
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(next):
		case <-sas.kick:
			vpnlog.Info("ENGINE", "rekeying now: the server deleted the ESP SA in use", nil)
		case <-sas.changed:
			// A new pair appeared (ours, or one the server rekeyed): re-plan
			// from it instead of rekeying on the old pair's schedule.
			next = schedule(sas.current().lifetime)
			continue
		}
		sas.expireOld(time.Now())
		if l := session().IKELifetime(); l > 0 && session().Age() > l {
			vpnlog.Error("ENGINE", "IKE SA lifetime has elapsed — the server may refuse this rekey", vpnlog.Fields{
				"ike_lifetime_s": int(l / time.Second),
			})
		}
		qm, err := rekey()
		if err == nil {
			err = sas.install(qm, time.Now())
		}
		if err != nil {
			failures++
			remaining := time.Until(sas.current().expires)
			ikeExpired := session().IKELifetime() > 0 && session().Age() > session().IKELifetime()
			vpnlog.Error("ENGINE", "ESP rekey failed — retrying", vpnlog.Fields{
				"err": err, "current_sa_remaining_s": int(remaining / time.Second),
				"failures": failures, "ike_expired": ikeExpired,
			})
			if giveUp != nil && rekeyHopeless(failures, remaining, ikeExpired) {
				// Waiting longer only ends with the SA expiring under live
				// traffic and a minute of silence before the watchdog notices;
				// a fresh negotiation now costs a couple of seconds.
				giveUp(fmt.Errorf("ESP rekey keeps failing (%d times, IKE SA expired: %v, current SA has %s left): %w", failures, ikeExpired, remaining.Round(time.Second), err))
				return
			}
			next = rekeyRetryEvery
			continue
		}
		failures = 0
		vpnlog.Info("ENGINE", "ESP SA rekeyed", vpnlog.Fields{
			"in_spi": fmt.Sprintf("%08x", qm.Inbound.SPI), "out_spi": fmt.Sprintf("%08x", qm.Outbound.SPI),
			"lifetime_s": int(qm.Lifetime / time.Second),
		})
		next = schedule(qm.Lifetime)
	}
}

// Rekey is hopeless — and a full reconnect is the better recovery — once the
// server has refused it while the IKE SA is past its lifetime (it is being, or
// has been, torn down), or after repeated failures with the SA close to
// expiring.
const (
	rekeyGiveUpFailures  = 3
	rekeyGiveUpRemaining = 5 * time.Minute
)

func rekeyHopeless(failures int, remaining time.Duration, ikeExpired bool) bool {
	if ikeExpired {
		return true
	}
	return failures >= rekeyGiveUpFailures && remaining < rekeyGiveUpRemaining
}

func rekeyDelay(lifetime time.Duration) time.Duration {
	d := lifetime / rekeyFraction
	if d < minRekeyWait {
		d = minRekeyWait
	}
	return d
}
