// Package engine orchestrates a full VPN connection: diagnostics, IKE
// Phase 1/2, ESP, L2TP, PPP, and routing/DNS — the state machine the CLI's
// connect/disconnect/repair commands drive.
package engine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"vpn/internal/dnsmgr"
	"vpn/internal/ike"
	"vpn/internal/ipsec"
	"vpn/internal/l2tp"
	"vpn/internal/netwatch"
	"vpn/internal/ppp"
	"vpn/internal/privilege"
	"vpn/internal/routing"
	"vpn/internal/state"
	"vpn/internal/sysbin"
	"vpn/internal/tun"
	"vpn/internal/vpnlog"
)

// LogPath returns where `logs`/`connect --verbose` write to.
func LogPath() string { return vpnlog.Path }

// Config is everything one connect attempt needs — assembled by the CLI
// from a Profile+Account+Keychain secrets, never persisted itself.
type Config struct {
	ProfileName  string
	Server       string
	ServerID     string
	AccountName  string
	Password     string
	PSK          string
	IKEProposals []string
	ESPProposals []string
	MTU          int
	FullTunnel   bool
	Timeout      time.Duration
	Verbose      bool
	RekeyAfter   time.Duration // >0: rekey this often instead of at half the SA lifetime
	KillSwitch   bool          // full tunnel only: block traffic while reconnecting instead of letting it leak
}

// errAbortedByDisconnect is Connect's internal sentinel for "a `disconnect`
// arrived while still negotiating and this process cancelled itself in
// response" — not a real connection failure, so it must never surface as a
// FAILED state/alert to the UI.
var errAbortedByDisconnect = errors.New("connect aborted: disconnected while negotiating")

// tunnelDropped is connectOnce's report that a tunnel that had come up was
// lost afterwards (the data plane stopped). Everything it had changed is
// already restored; Connect answers by reconnecting.
type tunnelDropped struct {
	cause  error
	uptime time.Duration
	// stale identifies the LNS-side session of the tunnel that was just lost:
	// when the network died first, we never told the server to close it, and it
	// would keep the account "already logged in" until its own echo timeout.
	stale       *staleSession
	lastLocalIP net.IP
}

func (e *tunnelDropped) Error() string {
	return fmt.Sprintf("tunnel dropped after %s: %v", e.uptime.Round(time.Second), e.cause)
}

func (e *tunnelDropped) Unwrap() error { return e.cause }

// staleSession is the LNS's tunnel/session ID pair of a session of ours that
// may still be alive on the server.
type staleSession struct {
	tunnel, session uint16
	// l2tpPort is the L2TP port that session ran on. The LNS finds a tunnel by
	// the peer's address and port as well as its ID, so the Terminate only
	// reaches the session from that same port: the attempt carrying it reuses it.
	l2tpPort uint16
	done     bool // already sent: one round of Terminate per record, not one per attempt
}

// staleTerminateAttempts / staleTerminateGap: the Terminate-Request is
// unreliable (a data message), so it is sent a few times.
const staleTerminateAttempts = 2

var (
	staleTerminateGap = 40 * time.Millisecond
	staleSettle       = 60 * time.Millisecond // fast non-blocking eviction
)

// evictStale asks the LNS to end a session of ours that was never closed, by
// sending an LCP Terminate-Request to it as if from the old client (same
// source address, the old peer tunnel/session IDs). Without this, after an
// abrupt network loss the reconnect is refused with "already logged in" until
// the server's pppd gives up on its unanswered echoes (about 75 s measured).
// Harmless if the session is already gone: the LNS ignores unknown IDs.
func evictStale(t l2tp.Transport, s *staleSession) {
	if s.done {
		return
	}
	s.done = true
	frame := ppp.Frame{Protocol: ppp.ProtoLCP, Payload: ppp.ControlPacket{Code: ppp.CodeTerminateRequest, Identifier: 0x7f}.Marshal()}
	raw := frame.Marshal()
	for i := 0; i < staleTerminateAttempts; i++ {
		if err := l2tp.SendDataTo(t, s.tunnel, s.session, raw); err != nil {
			vpnlog.Error("ENGINE", "could not send Terminate for the previous session", vpnlog.Fields{"err": err})
			return
		}
		time.Sleep(staleTerminateGap)
	}
	vpnlog.Info("ENGINE", "asked the server to end the previous session", vpnlog.Fields{"tunnel": s.tunnel, "session": s.session})
	time.Sleep(staleSettle)
}

// connectError is a failed stage of one connect attempt.
type connectError struct {
	stage, detail string
	err           error
}

func (e *connectError) Error() string { return fmt.Sprintf("%s: %s: %v", e.stage, e.detail, e.err) }
func (e *connectError) Unwrap() error { return e.err }

// Reconnect timing: after a drop or a failed reconnect attempt, wait
// reconnectMin, doubling up to reconnectMax. A tunnel that then stayed up for
// stableAfter counts as healthy again and resets the wait.
const (
	reconnectMin      = 2 * time.Second
	reconnectMax      = 20 * time.Second
	stableAfter       = time.Minute
	maxAuthRejections = 3 // consecutive real credential rejections before giving up
)

// A first connect whose negotiation merely times out — the LNS not answering
// SCCRQ, or never sending its CHAP challenge — is retried by the daemon a couple
// of times before the user is shown an error: the server does this
// intermittently (typically while it still holds an earlier session) and the
// very next attempt normally goes through. Credential rejections are never
// retried.
const (
	maxNegotiationRetries = 5
	negotiationRetryWait  = 3 * time.Second
)

// transientNegotiationFailure reports whether ce is such a timeout.
func transientNegotiationFailure(ce *connectError) bool {
	switch ce.stage {
	case "L2TP_TIMEOUT":
		return true
	case "LCP_FAILED", "PPP_AUTH_FAILURE", "IPCP_FAILURE":
		return strings.Contains(ce.Error(), "timed out")
	}
	return false
}

// Connect brings the tunnel up and keeps it up. The first attempt behaves
// as it always did: any failure is returned (and recorded as FAILED) so the
// user sees it. Once a tunnel has been established, though, losing it is not
// the end: the daemon tears down what is left and reconnects on its own,
// with backoff, for as long as it runs — a VPN that quietly stays dead while
// reporting "connected" (or that exits on the first hiccup) is worse than
// one that briefly reconnects. It ends only on SIGTERM/SIGINT (`disconnect`)
// or if the server keeps rejecting the credentials.
func Connect(cfg Config) error {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	// Registered before anything else — including before the PID that lets
	// `disconnect` find this process at all — and reused for the entire
	// call (every attempt, and the waits between them) instead of separate
	// signal.NotifyContexts, so there is exactly one place that decides
	// whether a termination was requested.
	sigCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	var (
		reconnecting bool
		reconnects   int
		wait         = reconnectMin
		authRejects  int
		negRetries   int           // first-connect negotiation timeouts retried so far
		stale        *staleSession // the last dropped tunnel's server-side session, until a new one replaces it
		lastLocalIP  net.IP        // retain assigned IP address across reconnects
		blocked      bool          // a kill-switch hold may have left blackhole routes that only we can remove
	)
	// A session an earlier run could not close (killed daemon, crash, reboot)
	// is ended by the first attempt, before it logs in.
	stale = loadLastSession(cfg.Server, time.Now())
	if stale != nil {
		vpnlog.Info("ENGINE", "found a session left by an earlier run — it will be ended before logging in", vpnlog.Fields{"tunnel": stale.tunnel, "session": stale.session})
	}
	releaseBlock := func() {
		if blocked {
			_ = privilege.Elevate(func() error { return routing.RestoreByServerIP(cfg.Server) })
			blocked = false
		}
	}
	defer releaseBlock() // every way out of Connect ends with the network back to normal
	for {
		// While retries remain, a failed first attempt is recorded as CONNECTING
		// (not FAILED), so the UI keeps showing progress instead of an alert.
		quiet := !reconnecting && negRetries < maxNegotiationRetries
		err := connectOnce(sigCtx, cfg, reconnecting, reconnects, quiet, stale, lastLocalIP)
		if err == nil {
			return nil // disconnected on request
		}

		var dropped *tunnelDropped
		var ce *connectError
		switch {
		case errors.As(err, &dropped):
			reconnecting = true
			stale = dropped.stale
			if dropped.lastLocalIP != nil {
				lastLocalIP = dropped.lastLocalIP
			}
			blocked = blocked || (cfg.KillSwitch && cfg.FullTunnel)
			reconnects++
			authRejects = 0
			if dropped.uptime >= stableAfter {
				wait = reconnectMin
			}
			vpnlog.Error("ENGINE", "tunnel lost — reconnecting", vpnlog.Fields{"err": dropped.cause, "uptime": dropped.uptime.Round(time.Second), "reconnects": reconnects, "retry_in": wait})
		case reconnecting && errors.As(err, &ce):
			if ce.stage == "PPP_AUTH_FAILURE" && !strings.Contains(ce.Error(), "already logged in") {
				authRejects++
				if authRejects >= maxAuthRejections {
					// Really being refused, not merely racing our own stale
					// session: stop instead of hammering the account.
					releaseBlock()
					return finalFailure(cfg, ce, "giving up reconnecting: the server keeps rejecting the login")
				}
			} else {
				authRejects = 0
			}
			vpnlog.Error("ENGINE", "reconnect attempt failed — will retry", vpnlog.Fields{"err": err, "retry_in": wait})
		case quiet && errors.As(err, &ce) && transientNegotiationFailure(ce):
			negRetries++
			wait = negotiationRetryWait
			vpnlog.Error("ENGINE", "connect attempt timed out during negotiation — retrying", vpnlog.Fields{"err": err, "retry": negRetries, "of": maxNegotiationRetries})
		case quiet && errors.As(err, &ce):
			// Not a timeout (e.g. wrong password): the quiet attempt recorded
			// CONNECTING, so record the real outcome before exiting.
			return finalFailure(cfg, ce, "connect failed — not retrying")
		default:
			return err
		}

		select {
		case <-sigCtx.Done():
			releaseBlock() // before the state flips to DISCONNECTED: the user sees "off" only once traffic flows again
			_ = privilege.Elevate(state.Clear)
			vpnlog.Info("ENGINE", "disconnected while waiting to reconnect", nil)
			return nil
		case <-time.After(wait):
		}
		if wait *= 2; wait > reconnectMax {
			wait = reconnectMax
		}
	}
}

// finalFailure records that connecting was given up on and returns the error.
// why is the log line: it used to say "the server keeps rejecting the login"
// for every failure, a network timeout or a local interface error included.
func finalFailure(cfg Config, ce *connectError, why string) error {
	failed := &state.State{
		Phase: state.PhaseFailed, Profile: cfg.ProfileName, Account: cfg.AccountName, Server: cfg.Server,
		FailStage: ce.stage, FailDetail: fmt.Sprintf("%s: %v", ce.detail, ce.err),
	}
	_ = privilege.Elevate(failed.Save)
	vpnlog.Error("ENGINE", why, vpnlog.Fields{"stage": ce.stage, "err": ce})
	return ce
}

// connectOnce runs the full stage sequence once and, on success, leaves the
// tunnel interface up, routes/DNS applied, and state.Save()'d as CONNECTED,
// then pumps packets until the tunnel is lost (returning *tunnelDropped) or
// a signal arrives (returning nil). On any failure, it restores whatever it
// had already changed before returning — a failed connect must never leave
// the machine half-configured. When reconnecting, failures are recorded as
// CONNECTING (with the reason) instead of FAILED, so the UI keeps showing an
// attempt in progress rather than raising an alert or racing its own retry.
//
// Privilege footprint: negotiation (IKE/L2TP/PPP — binding UDP/500, then
// parsing whatever the far end sends) and setup (opening utun, route/DNS
// changes) run under privilege.Elevate, since those genuinely need root on
// macOS. Once the tunnel is up, runDataPlane — the long-lived loop that
// continuously parses untrusted IP packets from the far end of the tunnel
// for as long as the VPN stays connected — runs at the caller's real,
// unprivileged UID: it only needs the already-open utun fd and ESP socket,
// neither of which requires root once open, so there's no reason for the
// process to keep holding root through what's normally the vast majority
// of a session's lifetime.
func connectOnce(sigCtx context.Context, cfg Config, reconnecting bool, reconnects int, quiet bool, stale *staleSession, requestedIP net.IP) error {
	st := &state.State{Phase: state.PhaseConnecting, Profile: cfg.ProfileName, Account: cfg.AccountName, Server: cfg.Server, Reconnecting: reconnecting, Reconnects: reconnects}
	var connectedAt time.Time

	var rtSnapshot *routing.Snapshot
	var dnsSnap *dnsmgr.Snapshot
	var l2tpTun *l2tp.Tunnel
	var dev *tun.Device
	var pppT *pppOverL2TP
	var ipcp ppp.NegotiatedIPCP
	var lcpMagic uint32                                          // ours, for answering and sending LCP Echo-Requests (PPP result)
	var startRekey func(ctx context.Context, giveUp func(error)) // set once Quick Mode succeeds
	var ikeSess *ike.Session                                     // the first IKE SA, set once Phase 1 succeeds
	var startReauth func(ctx context.Context, giveUp func(error))
	var sas *saSet              // set once Quick Mode succeeds
	var localIPOfSession net.IP // our outbound address for this attempt
	live := newLiveness()

	// Kill switch: while reconnecting a full tunnel, routes are not restored
	// after a failed attempt — the blackholes left by the previous drop stay,
	// so nothing leaks between attempts. The first attempt never holds: a
	// user whose very first connect fails gets their network back.
	blocking := cfg.KillSwitch && cfg.FullTunnel
	holdRoutes := blocking && reconnecting
	restoreRoutes := func() {
		if holdRoutes {
			if err := rtSnapshot.Blackhole(); err != nil {
				vpnlog.Error("ENGINE", "kill switch: could not keep traffic blocked", vpnlog.Fields{"err": err})
			}
			_ = rtSnapshot.RestoreKeepingBlackhole()
			return
		}
		_ = rtSnapshot.Restore()
	}

	// The IKE/ESP socket reader (ike.StartDataPhase) runs from Quick Mode
	// until Connect returns, on every path. Deliberately not a child of
	// sigCtx: a disconnect's teardown (PPP Terminate, CDN/StopCCN, IKE
	// Delete) runs after the signal, and must still hear the server's
	// answers — with the reader already gone, every Terminate-Ack was lost
	// and each disconnect waited out Terminate's full timeout.
	ikeCtx, stopIKE := context.WithCancel(context.Background())
	defer stopIKE()

	// Every IKE SA carrying ESP (one, or two during a re-authentication).
	mux := newIKESessions(ikeCtx)
	defer mux.closeAll()

	// Registered after stopIKE so it runs first, on every return path —
	// including each failure between Phase 1 and a working tunnel — and
	// tells the server to drop the IKE SA. Otherwise the server keeps the
	// half-open SA (and the account's session) until its own timeouts.
	defer func() {
		if ikeSess != nil {
			ikeSess.Close()
		}
	}()

	setupErr := privilege.Elevate(func() error {
		// vpnlog.Init has to run here, not before Elevate: /var/log/vpn.log
		// is root-owned (see internal/vpnlog), and this is the one point in
		// Connect that's actually privileged. Once open, the fd stays valid
		// for every later vpnlog call in this process regardless of euid —
		// Unix only checks permissions at open(2), not on each write.
		if err := vpnlog.Init(cfg.Verbose); err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		// Record PID together with the very first save, not only once
		// CONNECTED (as before) — otherwise a `disconnect` issued while
		// this process is still negotiating IKE/L2TP/PPP finds state.PID
		// still zero, can't signal this process, and just clears the state
		// file locally while this process keeps running and authenticating
		// in the background. A `connect` started right after then races
		// that orphan for the same account, which is what intermittently
		// produced spurious PPP auth failures / stale "already logged in"
		// sessions, and later state-file flicker, on rapid on/off/on
		// toggling: the orphan can finish and overwrite state.json to
		// CONNECTED well after the UI already believes it disconnected.
		st.PID = os.Getpid()
		_ = st.Save()

		fail := func(stage, detail string, err error) error {
			if sigCtx.Err() != nil {
				// Cancelled by our own signal handler above, not a real
				// failure — whatever was captured/applied so far has
				// already been restored at each call site below, exactly
				// like any other failure path.
				_ = state.Clear()
				vpnlog.Info("ENGINE", "connect aborted by disconnect", vpnlog.Fields{"stage": stage})
				return errAbortedByDisconnect
			}
			if reconnecting || quiet {
				// Still an attempt in progress, not a failure the user has to
				// act on: the reason rides along for `vpn status` and the log.
				st.Phase = state.PhaseConnecting
			} else {
				st.Phase = state.PhaseFailed
			}
			st.FailStage = stage
			// Include the real underlying error, not just the generic
			// stage label passed in `detail` ("PPP negotiation", "IKEv1
			// Phase 1 negotiation", ...). The UI's failure classification
			// (session-stale vs. wrong-credentials vs. IKE/route failure)
			// pattern-matches against this string for things like "already
			// logged in" or "CHAP authentication rejected" — without the
			// real text, every PPP-stage failure looked identical to the
			// UI regardless of whether the server actually said "wrong
			// password" or "you're already logged in", so the dedicated
			// stale-session auto-retry flow could never trigger.
			st.FailDetail = fmt.Sprintf("%s: %v", detail, err)
			_ = st.Save()
			vpnlog.Error(stage, detail, vpnlog.Fields{"err": err})
			return &connectError{stage: stage, detail: detail, err: err}
		}

		var err error
		if err := ike.ValidateESPProposals(cfg.ESPProposals); err != nil {
			return fail("IPSEC_FAILURE", "invalid ESP proposal in profile", err)
		}
		rtSnapshot, err = routing.Capture()
		if err != nil {
			return fail("ROUTE_FAILURE", "capture current routing state", err)
		}
		if holdRoutes {
			rtSnapshot.ExpectBlackhole()
		}

		serverIP, err := resolveServer(cfg.Server)
		if err != nil {
			return fail("DNS_FAILURE", "resolve VPN server", err)
		}
		if err := rtSnapshot.ProtectServer(serverIP.String()); err != nil {
			return fail("ROUTE_FAILURE", "protect VPN server route", err)
		}

		localIP := localOutboundIP(rtSnapshot.DefaultInterface)
		localIPOfSession = localIP
		if localIP == nil {
			return fail("ROUTE_FAILURE", "determine local outbound IP", fmt.Errorf("no IPv4 on %s", rtSnapshot.DefaultInterface))
		}

		ctx, cancel := context.WithTimeout(sigCtx, cfg.Timeout)
		defer cancel()

		sess, err := ike.EstablishPhase1(ctx, ike.Config{
			ServerHost: serverIP.String(),
			ServerID:   cfg.ServerID,
			PSK:        cfg.PSK,
			Proposals:  cfg.IKEProposals,
			L2TPPort:   l2tpPortFor(stale),
			LocalIP:    localIP,
		})
		if err != nil {
			restoreRoutes()
			return fail("IKE_TIMEOUT", "IKEv1 Phase 1 negotiation", err)
		}
		ikeSess = sess
		vpnlog.Info("ENGINE", "IKE Phase 1 established", vpnlog.Fields{"nat_detected": sess.NATDetected})

		qm, err := sess.EstablishQuickMode(cfg.ESPProposals, localIP, serverIP)
		if err != nil && sess.L2TPPort != ike.L2TPPort {
			// A server that only accepts L2TP from port 1701 refuses our own
			// port — with an error Notify (ike.ErrQuickModeRejected) or by not
			// answering at all — so any failure here is retried on 1701: never
			// worse than a client that always used it.
			vpnlog.Error("IKE", "Quick Mode on our own L2TP port failed — retrying on 1701", vpnlog.Fields{"port": sess.L2TPPort, "err": err})
			sess.L2TPPort = ike.L2TPPort
			qm, err = sess.EstablishQuickMode(cfg.ESPProposals, localIP, serverIP)
		}
		if err != nil {
			restoreRoutes()
			return fail("IPSEC_FAILURE", "Quick Mode (ESP SA) negotiation", err)
		}
		vpnlog.Info("ENGINE", "Quick Mode established", vpnlog.Fields{"in_spi": fmt.Sprintf("%08x", qm.Inbound.SPI), "out_spi": fmt.Sprintf("%08x", qm.Outbound.SPI), "l2tp_port": sess.L2TPPort})

		sas, err = newSASet(qm)
		if err != nil {
			restoreRoutes()
			return fail("IPSEC_FAILURE", "set up ESP SAs", err)
		}
		// From here on one goroutine owns the socket and demultiplexes ESP
		// from IKE — required for rekeying while traffic flows.
		events := ike.Events{
			DeleteESP: func(spis []uint32) {
				if sas.drop(spis) {
					vpnlog.Error("ENGINE", "server deleted the ESP SA currently in use — rekeying immediately", nil)
				}
			},
			// DeleteIKE is only logged by the reader itself: ESP normally keeps
			// flowing until its own lifetime ends, and the watchdog reconnects
			// the moment it stops. Reconnecting here would drop a working tunnel.

			// A rekey the server starts itself: answer it, and send from the
			// new pair once it exists (the old one stays valid for inbound).
			ESPProposals: cfg.ESPProposals,
			NewChildSA: func(qm *ike.QuickModeResult) {
				if err := sas.install(qm, time.Now()); err != nil {
					vpnlog.Error("ENGINE", "could not install the SA pair from a server-initiated rekey", vpnlog.Fields{"err": err})
					return
				}
				vpnlog.Info("ENGINE", "ESP SA rekeyed by the server", vpnlog.Fields{
					"in_spi": fmt.Sprintf("%08x", qm.Inbound.SPI), "out_spi": fmt.Sprintf("%08x", qm.Outbound.SPI), "lifetime_s": int(qm.Lifetime / time.Second),
				})
			},
		}
		sess.StartDataPhase(ikeCtx, events)
		mux.add(sess)
		startRekey = func(ctx context.Context, giveUp func(error)) {
			// Rekeys always go over the newest IKE SA (mux.current), which
			// changes when a re-authentication swaps it.
			go runRekey(ctx, sas, func() ikeSession { return mux.current() }, func() (*ike.QuickModeResult, error) {
				return mux.current().RekeyQuickMode(cfg.ESPProposals, localIP, serverIP)
			}, qm.Lifetime, cfg.RekeyAfter, giveUp)
		}
		// Re-authentication: build a fresh IKE SA (and its Quick Mode) next to
		// the live one before the old one's lifetime runs out, then move over.
		startReauth = func(ctx context.Context, giveUp func(error)) {
			establish := func(ectx context.Context) (ikeSession, *ike.QuickModeResult, error) {
				var next *ike.Session
				var nqm *ike.QuickModeResult
				// Binding the IKE ports needs root, like the first Phase 1.
				err := privilege.Elevate(func() error {
					tctx, cancel := context.WithTimeout(ectx, cfg.Timeout)
					defer cancel()
					s2, err := ike.EstablishPhase1(tctx, ike.Config{
						ServerHost: serverIP.String(),
						ServerID:   cfg.ServerID,
						PSK:        cfg.PSK,
						Proposals:  cfg.IKEProposals,
						LocalIP:    localIP,
						Reauth:     true,
						L2TPPort:   sess.L2TPPort, // the L2TP tunnel keeps running on it
					})
					if err != nil {
						return fmt.Errorf("IKE Phase 1: %w", err)
					}
					q, err := s2.EstablishQuickMode(cfg.ESPProposals, localIP, serverIP)
					if err != nil {
						s2.Close()
						return fmt.Errorf("Quick Mode: %w", err)
					}
					s2.StartDataPhase(ikeCtx, events)
					next, nqm = s2, q
					return nil
				})
				if err != nil {
					return nil, nil, err
				}
				return next, nqm, nil
			}
			go runReauth(ctx, mux, sas, establish, giveUp)
		}
		espT := &espTransport{
			l2tpPort: sess.L2TPPort,
			mux:      mux,
			sas:      sas,
			live:     live,
			repairRoute: func() error {
				return privilege.Elevate(func() error {
					return rtSnapshot.ProtectServer(serverIP.String())
				})
			},
		}

		if stale != nil {
			evictStale(espT, stale)
		}
		hostName, _ := localHostName()
		l2tpTun, err = l2tp.Establish(ctx, espT, l2tp.Config{HostName: hostName, Timeout: cfg.Timeout})
		if err != nil {
			restoreRoutes()
			return fail("L2TP_TIMEOUT", "L2TP tunnel/session establishment", err)
		}
		vpnlog.Info("ENGINE", "L2TP session established", nil)

		pppT = &pppOverL2TP{tun: l2tpTun}
		mru := uint16(cfg.MTU)
		if mru == 0 {
			mru = 1280
		}
		pppResult, err := ppp.Run(ctx, pppT, ppp.Config{MRU: mru, Username: cfg.AccountName, Password: cfg.Password, RequestedIP: requestedIP, Timeout: cfg.Timeout})
		if err != nil {
			l2tpTun.Close()
			restoreRoutes()
			return fail(pppFailStage(err), "PPP negotiation", err)
		}
		if pppResult.MRU > 0 {
			mru = pppResult.MRU
		}
		ipcp = pppResult.IPCP
		lcpMagic = pppResult.Magic
		peerIPStr := "<none>"
		if ipcp.PeerIP != nil && !ipcp.PeerIP.IsUnspecified() {
			peerIPStr = ipcp.PeerIP.String()
		}
		vpnlog.Info("ENGINE", "PPP OPENED", vpnlog.Fields{"local_ip": ipcp.LocalIP.String(), "peer_ip": peerIPStr, "mru": mru})

		// Everything below is teardown-on-failure just like the stages
		// above, but now there are more resources (utun device, interface
		// config, DNS) to unwind in reverse order if any step fails.
		dev, err = tun.Open()
		if err != nil {
			ppp.Terminate(pppT, 1)
			l2tpTun.Close()
			restoreRoutes()
			return fail("TUN_FAILURE", "open utun device", err)
		}
		teardownPartial := func() {
			if holdRoutes { // swap to blackholes before the interface disappears: no leak window
				_ = rtSnapshot.Blackhole()
			}
			dev.Close()
			ppp.Terminate(pppT, 1)
			l2tpTun.Close()
			if dnsSnap != nil {
				_ = dnsSnap.Restore()
			}
			restoreRoutes()
		}

		peer := ipcp.PointToPointPeer() // the LNS's address, or a stand-in when it has none
		if err := routing.ConfigureP2PInterface(dev.Name, ipcp.LocalIP.String(), peer.String(), int(mru)); err != nil {
			teardownPartial()
			return fail("TUN_FAILURE", "configure utun interface", err)
		}
		vpnlog.Info("ENGINE", "utun interface up", vpnlog.Fields{"device": dev.Name, "local_ip": ipcp.LocalIP.String(), "peer_ip": peer.String(), "peer_ip_from_server": peer.Equal(ipcp.PeerIP), "mtu": mru})

		if cfg.FullTunnel {
			if err := rtSnapshot.ApplyFullTunnel(dev.Name); err != nil {
				teardownPartial()
				return fail("ROUTE_FAILURE", "apply full-tunnel default routes", err)
			}
			// Adding split-default routes can make macOS discard the existing
			// cloned host route. Re-add the static exception before ESP sends.
			if err := rtSnapshot.ProtectServer(serverIP.String()); err != nil {
				teardownPartial()
				return fail("ROUTE_FAILURE", "protect VPN server after full-tunnel routes", err)
			}
			vpnlog.Info("ENGINE", "full-tunnel routes applied", vpnlog.Fields{"device": dev.Name})
		}

		dnsServers := dnsServerStrings(ipcp)
		if len(dnsServers) > 0 && !cfg.FullTunnel {
			if err := rtSnapshot.RouteHostsViaTunnel(dnsServers, dev.Name); err != nil {
				teardownPartial()
				return fail("ROUTE_FAILURE", "route pushed DNS servers through the tunnel", err)
			}
		}
		if len(dnsServers) > 0 {
			service, _ := dnsmgr.ServiceForInterface(rtSnapshot.DefaultInterface)
			snap, _ := dnsmgr.Capture(service)
			if snap == nil {
				snap = &dnsmgr.Snapshot{Service: service}
			}
			snap.TunIface = dev.Name
			if err := snap.Apply(dnsServers); err != nil {
				teardownPartial()
				return fail("DNS_FAILURE", "apply LNS-provided DNS servers", err)
			}
			dnsSnap = snap
			st.DNSService = snap.Service
			st.DNSServers = snap.Servers
			st.DNSApplied = true
			vpnlog.Info("ENGINE", "DNS applied", vpnlog.Fields{"service": service, "servers": dnsServers})
		} else if cfg.FullTunnel {
			// Not an error — some LNSes simply don't push DNS — but the
			// system keeps its current resolvers, and one on the local
			// network (e.g. the Wi-Fi router) is reached via its on-link
			// route, outside the tunnel: every lookup is visible to that
			// network.
			st.Warnings = append(st.Warnings, warnNoPushedDNS)
			vpnlog.Error("ENGINE", warnNoPushedDNS, nil)
		}

		st.Phase = state.PhaseConnected
		st.FailStage, st.FailDetail = "", "" // a reconnect attempt's last error is history once it worked
		st.Reconnecting = false              // up again; st.Reconnects keeps the count
		connectedAt = time.Now()
		// st.PID was already recorded at the top of this closure.
		st.TunDevice = dev.Name
		st.LocalIP = ipcp.LocalIP.String()
		st.SavedRoutes = true
		if err := st.Save(); err != nil {
			teardownPartial()
			return fail("STATE_FAILURE", "persist connected state", err)
		}
		return nil
	})
	if setupErr != nil {
		if errors.Is(setupErr, errAbortedByDisconnect) {
			// A deliberate, in-flight disconnect — already cleaned up and
			// cleared inside fail() above; report success to the caller
			// (the CLI's own `disconnect` is what's waiting on this).
			return nil
		}
		return setupErr
	}
	{
		pt, ps := l2tpTun.PeerIDs()
		saveLastSession(cfg.Server, pt, ps, ikeSess.L2TPPort)
	}
	vpnlog.Info("ENGINE", "VPN connected", vpnlog.Fields{"local_ip": ipcp.LocalIP.String(), "device": dev.Name, "mtu": cfg.MTU, "full_tunnel": cfg.FullTunnel, "reconnects": reconnects,
		"ike_lifetime_s": int(mux.current().IKELifetime() / time.Second), "esp_lifetime_s": int(sas.current().expires.Sub(connectedAt) / time.Second)})

	// Everything that only makes sense while the tunnel is up stops together
	// when it is lost, and *before* teardown: a route watcher or rekey still
	// running while routes/SAs are being restored would undo the restore.
	dpCtx, stopDP := context.WithCancel(sigCtx) // a disconnect stops the data plane at once
	dropCh := make(chan error, 2)

	// macOS can silently reap the routes ProtectServer/ApplyFullTunnel just
	// installed at any point during a long-lived session (see routing.go's
	// Watch doc comment) — re-assert them periodically instead of trusting
	// they stay in place for the whole connection.
	kickRoutes := make(chan struct{}, 1)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		rtSnapshot.Watch(dpCtx, privilege.Elevate, 0, kickRoutes)
	}()

	// Liveness watchdog: keeps the NAT mapping and the LNS's idle timers fed,
	// and is the only thing that declares the peer dead (see liveness.go).
	var echoMu sync.Mutex
	var echoID uint8
	probe := func() { // shared by the watchdog and the network-event handler
		echoMu.Lock()
		echoID++
		id := echoID
		echoMu.Unlock()
		if err := mux.keepalive(); err != nil {
			vpnlog.Error("ENGINE", "NAT keepalive send failed", vpnlog.Fields{"err": err})
		}
		// The request carries OUR negotiated Magic-Number (0 if the peer rejected
		// it), as RFC 1661 §5.8 requires — pppd treats its own magic coming back
		// as a loop.
		magic := make([]byte, 4)
		binary.BigEndian.PutUint32(magic, lcpMagic)
		req := ppp.ControlPacket{Code: ppp.CodeEchoRequest, Identifier: id, Data: magic}
		if err := pppT.SendFrame(ppp.ProtoLCP, req.Marshal()); err != nil {
			vpnlog.Error("ENGINE", "LCP echo send failed", vpnlog.Fields{"err": err})
		}
		// Also send an L2TP Hello keepalive (RFC 2661 §5.8). Servers that do not
		// implement or answer PPP LCP Echo-Requests (common with firewalls/routers
		// that omit Peer IP in IPCP) will still acknowledge L2TP control messages
		// with a ZLB or Hello, keeping the ESP transport active and proving responsiveness.
		if l2tpTun != nil {
			if err := l2tpTun.SendHello(); err != nil {
				vpnlog.Debug("ENGINE", "L2TP hello send failed", vpnlog.Fields{"err": err})
			}
		}
	}
	drop := func(err error) {
		select {
		case dropCh <- err:
		default:
		}
	}
	startRekey(dpCtx, drop)
	startReauth(dpCtx, drop)
	go func() {
		reports := 0
		report := func(idle time.Duration) {
			// Keep the record's liveness stamp fresh (one small write per few minutes).
			if reports++; time.Duration(reports)*keepaliveEvery*statsEvery >= lastSessionRefresh {
				reports = 0
				pt, ps := l2tpTun.PeerIDs()
				saveLastSession(cfg.Server, pt, ps, ikeSess.L2TPPort)
			}
			cur := sas.current()
			vpnlog.Info("ENGINE", "tunnel alive", vpnlog.Fields{
				"uptime":         time.Since(connectedAt).Round(time.Second).String(),
				"rx_packets":     live.rx.Load(),
				"tx_packets":     live.tx.Load(),
				"esp_in_spi":     fmt.Sprintf("%08x", cur.in.SPI),
				"esp_out_spi":    fmt.Sprintf("%08x", cur.out.SPI),
				"esp_expires_in": time.Until(cur.expires).Round(time.Second).String(),
				"ike_age":        mux.current().Age().Round(time.Second).String(),
			})
		}
		if err := watchdog(dpCtx, keepaliveEvery, deadAfter, live, probe, report); err != nil {
			drop(err)
		}
	}()

	// Kernel network events (route deleted, link/address changed) — reacts to
	// a Wi-Fi roam or wake from sleep in seconds instead of waiting out the
	// watchdog, and re-adds reaped routes right away instead of on a timer.
	if events, err := netwatch.Subscribe(dpCtx); err != nil {
		vpnlog.Error("ENGINE", "network event feed unavailable — relying on the liveness watchdog and slow route checks", vpnlog.Fields{"err": err})
	} else {
		go watchNetwork(dpCtx, events, localIPOfSession, live, probe, kickRoutes, drop, localAddrPresent)
	}

	pumpErr := runDataPlane(dpCtx, dev, pppT, lcpMagic, dropCh)
	stopDP()
	<-watchDone
	uptime := time.Since(connectedAt)

	// Tear down on the way out no matter why the pump stopped (signal or
	// error) — the disconnect/repair CLI paths exist for when this process
	// is no longer around to do it itself, not as the primary mechanism.
	return privilege.Elevate(func() error {
		lost := pumpErr != nil && sigCtx.Err() == nil
		holdOnExit := lost && blocking
		if holdOnExit {
			// Swap the tunnel's routes for blackholes *before* the interface
			// goes away, so there is no moment when traffic can leak.
			if err := rtSnapshot.Blackhole(); err != nil {
				vpnlog.Error("ENGINE", "kill switch: could not block traffic", vpnlog.Fields{"err": err})
			}
		}
		dev.Close()
		if lost && live.idle() > 2*keepaliveEvery {
			// The peer has stopped answering, so waiting for its Terminate-Ack
			// only delays the reconnect (the same goes for the L2TP messages
			// below, which never wait).
			ppp.TerminateNoWait(pppT, 1)
		} else {
			ppp.Terminate(pppT, 1)
		}
		l2tpTun.Close()
		if dnsSnap != nil {
			_ = dnsSnap.Restore()
		}
		if holdOnExit {
			_ = rtSnapshot.RestoreKeepingBlackhole()
		} else {
			_ = rtSnapshot.Restore()
		}

		if lost {
			// Stopped for a reason other than the signal we were waiting
			// for. Say honestly what happens to the traffic meanwhile.
			exposure := "traffic goes over the normal network WITHOUT VPN protection until it is back"
			if holdOnExit {
				exposure = "all internet traffic is BLOCKED (kill switch) until it is back"
			}
			dropping := &state.State{
				Phase:        state.PhaseConnecting,
				Reconnecting: true,
				Reconnects:   reconnects + 1,
				PID:          os.Getpid(),
				Profile:      cfg.ProfileName,
				Account:      cfg.AccountName,
				Server:       cfg.Server,
				FailStage:    "TUNNEL_DROPPED",
				FailDetail:   fmt.Sprintf("tunnel dropped (%v) — reconnecting; %s", pumpErr, exposure),
			}
			_ = dropping.Save()
			vpnlog.Error("ENGINE", "data plane stopped unexpectedly", vpnlog.Fields{"err": pumpErr, "exposure": exposure})
			pt, ps := l2tpTun.PeerIDs()
			return &tunnelDropped{cause: pumpErr, uptime: uptime, stale: &staleSession{tunnel: pt, session: ps, l2tpPort: ikeSess.L2TPPort}, lastLocalIP: ipcp.LocalIP}
		}
		clearLastSession() // closed properly: nothing left on the server to end
		_ = state.Clear()
		vpnlog.Info("ENGINE", "disconnected", nil)
		return nil
	})
}

// warnNoPushedDNS is surfaced by connect/status when the LNS assigned no DNS
// servers under full tunnel (see Connect).
const warnNoPushedDNS = "VPN server pushed no DNS servers: DNS lookups keep using this network's resolvers, and any on the local network bypass the VPN"

// newESPSA turns one direction of Quick Mode's result into the data-plane
// SA for exactly the transform that was negotiated.
func newESPSA(c ike.ChildSA) (*ipsec.SA, error) {
	var cipher ipsec.Cipher
	switch c.Transform.Encryption {
	case ike.Enc3DES:
		cipher = ipsec.Cipher3DESCBC
	case ike.EncAES:
		cipher = ipsec.CipherAESCBC
	default:
		return nil, fmt.Errorf("negotiated ESP encryption %d has no data-plane implementation", c.Transform.Encryption)
	}
	var integrity ipsec.Integrity
	switch c.Transform.Hash {
	case ike.HashMD5:
		integrity = ipsec.IntegHMACMD5_96
	case ike.HashSHA1:
		integrity = ipsec.IntegHMACSHA1_96
	case ike.HashSHA256:
		integrity = ipsec.IntegHMACSHA256_128
	case ike.HashSHA384:
		integrity = ipsec.IntegHMACSHA384_192
	case ike.HashSHA512:
		integrity = ipsec.IntegHMACSHA512_256
	default:
		return nil, fmt.Errorf("negotiated ESP integrity %d has no data-plane implementation", c.Transform.Hash)
	}
	return ipsec.NewSA(c.SPI, cipher, integrity, c.EncKey, c.AuthKey)
}

// dnsServerStrings collects the LNS-provided DNS servers as strings,
// skipping any that are absent or 0.0.0.0 (the LNS declining to supply
// one, encoded the same way a IPCP request placeholder is).
func dnsServerStrings(ipcp ppp.NegotiatedIPCP) []string {
	var out []string
	for _, ip := range []net.IP{ipcp.PrimaryDNS, ipcp.SecondDNS} {
		if ip == nil || ip.IsUnspecified() {
			continue
		}
		out = append(out, ip.String())
	}
	return out
}

// utunReadBuf is the largest packet read from utun in one call. The tunnel MTU
// is 1280–1400, so 16 KiB is generous; 64 KiB just held memory for nothing.
const utunReadBuf = 16 << 10

// ioFailureLimit is how long a data-plane read/write may fail continuously,
// with no success in between, before the tunnel is given up on. Shorter
// blips (a Wi-Fi roam, a route macOS is re-adding, a momentarily full
// buffer) just cost the packets in flight, which TCP and PPP recover.
const ioFailureLimit = 30 * time.Second

// runDataPlane bridges the utun device and the PPP/L2TP/ESP stack: every
// IP packet read from one side is written to the other. It blocks until ctx
// is cancelled, a fatal error stops either direction, or a message arrives
// on dropCh (the liveness watchdog's verdict). Individual packet errors are
// not fatal — see ioFailureLimit.
func runDataPlane(ctx context.Context, dev *tun.Device, pppT *pppOverL2TP, lcpMagic uint32, dropCh <-chan error) error {
	errCh := make(chan error, 2)
	fatal := func(err error) {
		select {
		case errCh <- err:
		default:
		}
	}
	// Stop the receive side before returning: the caller's teardown reads the
	// same PPP stream (LCP Terminate-Ack), and a pump goroutine still
	// consuming it would steal the reply.
	ctx, cancel := context.WithCancel(ctx)
	var recvDone sync.WaitGroup
	defer func() {
		cancel()
		recvDone.Wait()
	}()

	// utun -> Send Queue (buffered) -> ESP send worker
	sendCh := make(chan []byte, 4096)

	// Worker: Encrypts and writes UDP packets as fast as CPU/socket allows
	go func() {
		sends := &failStreak{limit: ioFailureLimit}
		for {
			select {
			case <-ctx.Done():
				return
			case pkt, ok := <-sendCh:
				if !ok {
					return
				}
				if err := pppT.SendIP(pkt); err != nil {
					isFatal, logIt := sends.fail(time.Now())
					if logIt {
						vpnlog.Error("ENGINE", "send failed — dropping packets until it recovers", vpnlog.Fields{"err": err})
					}
					if isFatal {
						fatal(fmt.Errorf("sending to the server failed for %s: %w", ioFailureLimit, err))
						return
					}
					continue
				}
				if sends.n > 0 {
					vpnlog.Info("ENGINE", "send recovered", vpnlog.Fields{"failed_packets": sends.n})
					sends.ok()
				}
			}
		}
	}()

	// Reader: Reads from utun without blocking on network/crypto
	go func() {
		defer close(sendCh)
		buf := make([]byte, utunReadBuf)
		reads := &failStreak{limit: ioFailureLimit}
		for {
			n, err := dev.Read(buf)
			if err != nil {
				if errors.Is(err, syscall.EBADF) || ctx.Err() != nil {
					fatal(fmt.Errorf("read utun: %w", err)) // closed under us: normal teardown
					return
				}
				isFatal, logIt := reads.fail(time.Now())
				if logIt {
					vpnlog.Error("ENGINE", "read utun failed — retrying", vpnlog.Fields{"err": err})
				}
				if isFatal {
					fatal(fmt.Errorf("read utun failing for %s: %w", ioFailureLimit, err))
					return
				}
				time.Sleep(50 * time.Millisecond)
				continue
			}
			reads.ok()
			// Only IPv4 is negotiated (IPCP, never IPV6CP): framing any
			// other packet as ppp.ProtoIP would hand the LNS garbage.
			if n == 0 || buf[0]>>4 != 4 {
				continue
			}
			pkt := make([]byte, n)
			copy(pkt, buf[:n])
			select {
			case sendCh <- pkt:
			case <-ctx.Done():
				return
			}
		}
	}()

	// ESP -> L2TP -> PPP -> utun
	recvDone.Add(1)
	go func() {
		defer recvDone.Done()
		writes := &failStreak{limit: ioFailureLimit}
		for {
			proto, payload, err := pppT.RecvFrame(ctx)
			if err != nil {
				// Only a gone transport (or our own cancellation) gets here:
				// bad frames are skipped inside RecvFrame.
				fatal(fmt.Errorf("recv PPP frame: %w", err))
				return
			}
			switch proto {
			case ppp.ProtoIP:
				if _, err := dev.Write(payload); err != nil {
					isFatal, logIt := writes.fail(time.Now())
					if logIt {
						vpnlog.Error("ENGINE", "write utun failed — dropping packets until it recovers", vpnlog.Fields{"err": err})
					}
					if isFatal || errors.Is(err, syscall.EBADF) {
						fatal(fmt.Errorf("write utun: %w", err))
						return
					}
					continue
				}
				writes.ok()
			case ppp.ProtoLCP:
				// The LNS keeps sending LCP Echo-Requests as a keepalive for the
				// whole session and ends it once enough go unanswered (its reply
				// must carry OUR magic number, see ppp.EchoReply); it can also
				// retransmit its Configure-Request if our Ack was lost. Same
				// handling as during authentication.
				if pkt, err := ppp.ParseControlPacket(payload); err == nil && pkt.Code == ppp.CodeTerminateRequest {
					// The server is ending the PPP session (kicked us, idle
					// timeout, another login). Say so in the log and answer
					// it, then let Connect reconnect: nothing more will
					// arrive on this session.
					vpnlog.Error("ENGINE", "server sent LCP Terminate-Request — it is closing this PPP session", nil)
					ack := ppp.ControlPacket{Code: ppp.CodeTerminateAck, Identifier: pkt.Identifier}
					_ = pppT.SendFrame(ppp.ProtoLCP, ack.Marshal())
					fatal(fmt.Errorf("server terminated the PPP session (LCP Terminate-Request)"))
					return
				}
				ppp.HandleOpenedLCP(pppT, payload, lcpMagic)
			}
			// Other protocols (e.g. IPV6CP, which this client never
			// negotiates) are silently ignored — the peer already knows we
			// didn't ack them.
		}
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-dropCh:
		return err
	case err := <-errCh:
		return err
	}
}

func pppFailStage(err error) string {
	msg := err.Error()
	for _, stage := range []string{"LCP_FAILED", "PPP_AUTH_FAILURE", "IPCP_FAILURE"} {
		if len(msg) >= len(stage) && msg[:len(stage)] == stage {
			return stage
		}
	}
	return "LCP_FAILED"
}

func localHostName() (string, error) {
	return "l2tp-cli", nil
}

// connectLockPath is an flock'd file that serializes connect/disconnect's
// session-transition critical sections *across processes* — `connect` and
// `disconnect` are always separate OS processes (privilege.Elevate's own
// mutex only serializes goroutines within one process), so without this,
// a `disconnect` invoked right after a `connect` could run concurrently
// with that connect's own fork-and-record-PID step and simply find
// nothing yet to kill (see ClaimNewConnect's doc comment).
func connectLockPath() string { return filepath.Join(state.Dir, "connect.lock") }

// WithConnectLock runs fn with the cross-process connect lock held. Held
// only around the *transition* (deciding what's currently running, and
// killing/claiming it) — never around an entire connection attempt, so a
// disconnect can still promptly interrupt a slow negotiation once that
// negotiation's own claim has been recorded and this lock released.
func WithConnectLock(fn func() error) error {
	f, err := openLockFile()
	if err != nil {
		return fmt.Errorf("open connect lock: %w", err)
	}
	return runLocked(f, fn)
}

// WithConnectLockIfAvailable is WithConnectLock for changes that must not
// interleave with a connect starting (editing or removing a profile), but that
// also have to keep working without root (a non-setuid build): there no connect
// can be running either, so nothing needs serializing and fn simply runs. Only
// that one case skips the lock — any other failure to open it (disk full, a
// broken state directory, ...) is transient and says nothing about whether a
// connect elsewhere holds the lock, so it is returned, not ignored.
func WithConnectLockIfAvailable(fn func() error) error {
	f, err := openLockFile()
	if errors.Is(err, privilege.ErrNotPrivileged) {
		return fn()
	}
	if err != nil {
		return fmt.Errorf("open connect lock: %w", err)
	}
	return runLocked(f, fn)
}

func runLocked(f *os.File, fn func() error) error {
	defer f.Close()
	// flock, not privilege.Elevate's mutex: this needs to serialize across
	// separate `vpn` processes, which an in-process mutex cannot do. Once
	// open, flock/close need no privilege regardless of who opened the fd
	// (permissions are only checked at open(2) — same reasoning as
	// vpnlog.Init's comment).
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire connect lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func openLockFile() (*os.File, error) {
	var f *os.File
	err := privilege.Elevate(func() error {
		if err := os.MkdirAll(state.Dir, 0o755); err != nil {
			return err
		}
		_ = os.Chmod(state.Dir, 0o755)
		var err error
		f, err = os.OpenFile(connectLockPath(), os.O_CREATE|os.O_RDWR, 0o644)
		if err == nil {
			_ = os.Chmod(connectLockPath(), 0o644)
		}
		return err
	})
	return f, err
}

// Disconnect tears down a running connection. `connect` runs in the
// foreground (or daemonized) for as long as the tunnel is up — see its
// runDataPlane loop — so the normal path here is to signal that live
// process and let it do its own graceful teardown (closing the utun
// device, restoring routes/DNS, clearing state) exactly like a Ctrl-C
// would. Only if that process is gone (crashed, killed -9) does this fall
// back to redoing the restore itself from what was last recorded on disk.
func Disconnect() error {
	return WithConnectLock(func() error { return killExisting(true) })
}

// AlreadyServing reports whether a live connect daemon is already handling
// this profile/account: connected, or reconnecting on its own. A second
// `vpn connect` for the same target must leave it alone — tearing down a
// working session (and the server-side login with it) to negotiate an
// identical one is what turned a stray connect into a dropped connection.
// Callers must already hold WithConnectLock.
func AlreadyServing(profile, account string) (*state.State, bool) {
	st, err := state.Load()
	if err != nil || st.PID <= 0 || !processAlive(st.PID) {
		return nil, false
	}
	if st.Profile != profile || st.Account != account {
		return nil, false
	}
	if st.Phase == state.PhaseConnected || (st.Phase == state.PhaseConnecting && st.Reconnecting) {
		return st, true
	}
	return nil, false
}

// ProfileInUse reports whether a live daemon is connected (or connecting, or
// reconnecting) through profile. Editing or deleting such a profile would pull
// the server address, account or secrets out from under the running tunnel, so
// callers refuse until it is disconnected. A stale state file whose daemon is
// gone does not count.
func ProfileInUse(profile string) (*state.State, bool) {
	st, err := state.Load()
	if err != nil || st.PID <= 0 || !processAlive(st.PID) || st.Profile != profile {
		return nil, false
	}
	if st.Phase == state.PhaseConnected || st.Phase == state.PhaseConnecting {
		return st, true
	}
	return nil, false
}

// PrepareNewConnect tears down any previous connect/connected session
// before a new one starts. `connect` calls this itself while holding
// WithConnectLock (see cli.cmdConnect), before forking its daemon, so two
// negotiation attempts for the same account can never run concurrently.
// That used to be the caller's job (the menu bar app ran its own
// `disconnect` immediately before every `connect`, followed by an
// unconditional `pkill -9` as a blanket safety net); doing it here
// instead, synchronously and under the same lock the new daemon's PID
// gets claimed under, closes two gaps that approach had: a caller could
// race this process's own state.json write on a very fast reconnect, and
// SIGKILL bypassed graceful cancellation even for a process that would
// have responded to SIGTERM within milliseconds — which could catch it
// mid-PPP-authentication and leave a session the server still considers
// logged in, exactly what produced the intermittent "already logged in" /
// CHAP-rejected failures on rapid on/off/on toggling. Silent when there is
// nothing to tear down. Callers must already hold WithConnectLock.
func PrepareNewConnect() error {
	return killExisting(false)
}

// ClaimNewConnect blocks (briefly) until state.json shows childPID as a
// live CONNECTING session — i.e. the just-forked daemon has recorded
// itself — so the caller can safely release WithConnectLock knowing a
// disconnect issued right after this returns will always find and signal
// it. Without this confirmation, releasing the lock immediately after
// forking would reopen the exact race this lock exists to close: a
// disconnect could run in the gap between the fork and the child actually
// writing its PID, see nothing to kill, and leave the child running
// unnoticed. Callers must already hold WithConnectLock.
func ClaimNewConnect(childPID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		st, err := state.Load()
		if err == nil && st.Phase == state.PhaseConnecting && st.PID == childPID {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("connect process (pid %d) did not report itself within %s", childPID, timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// killExisting is disconnect/PrepareNewConnect's shared implementation.
// verbose controls the user-facing progress prints — off when `connect`
// calls this on its own behalf, so it doesn't narrate a disconnect the
// user never asked for. Callers must already hold WithConnectLock.
func killExisting(verbose bool) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	if st.Phase != state.PhaseConnected && st.Phase != state.PhaseConnecting {
		if verbose {
			fmt.Println("Already disconnected.")
		}
		return nil
	}
	return privilege.Elevate(func() error {
		if st.PID > 0 && processAlive(st.PID) {
			if err := syscall.Kill(st.PID, syscall.SIGTERM); err != nil {
				return fmt.Errorf("signal running connect process (pid %d): %w", st.PID, err)
			}
			for i := 0; i < 250; i++ { // up to ~5s for its own graceful teardown (fast 20ms polling)
				time.Sleep(20 * time.Millisecond)
				cur, err := state.Load()
				if err == nil && cur.Phase == state.PhaseDisconnected {
					if verbose {
						fmt.Println("Disconnected.")
					}
					return nil
				}
			}
			// Unresponsive to SIGTERM for a full 5s — genuinely wedged,
			// not merely mid-negotiation (Connect's signal handler reacts
			// to SIGTERM within a retry-loop tick, well under this).
			// Escalate to SIGKILL of this exact PID — never a wildcard
			// process-name match — so a stuck connection can still always
			// be cleared without any risk of hitting an unrelated process
			// that happens to share `vpn connect` in its command line.
			vpnlog.Error("ENGINE", "connect process unresponsive to SIGTERM, escalating to SIGKILL", vpnlog.Fields{"pid": st.PID})
			_ = syscall.Kill(st.PID, syscall.SIGKILL)
			restoreRecorded(st)
			if verbose {
				fmt.Println("Force-terminated an unresponsive connection.")
			}
			return state.Clear()
		}
		// No live process to signal — restore from what was last recorded.
		restoreRecorded(st)
		return state.Clear()
	})
}

// Repair restores routing/DNS state after an abnormal termination, without
// needing credentials — it only ever removes state this client could have
// added (the two split-default override routes, the host route to
// whatever server address was last recorded, and the DNS servers it
// pushed). It refuses to act while a `connect` process is still alive —
// use `disconnect` for that, which lets that process tear itself down
// instead of racing it.
func Repair() error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	if st.PID > 0 && processAlive(st.PID) {
		return fmt.Errorf("connect (pid %d) is still running — use `vpn disconnect` instead", st.PID)
	}
	return privilege.Elevate(func() error {
		dnsmgr.CleanPersistentSettings()
		_ = (&dnsmgr.Snapshot{}).Restore()
		if st.Server == "" {
			fmt.Println("Cleaned any leftover DNS and routes — network is clean.")
			return state.Clear()
		}
		restoreRecorded(st)
		fmt.Printf("Restored routing/DNS state for %s.\n", st.Server)
		return state.Clear()
	})
}

// restoreRecorded redoes routing.Restore/dnsmgr.Restore purely from what
// was last persisted to disk — the only option once the process that held
// the live *routing.Snapshot/*dnsmgr.Snapshot objects is no longer around.
func restoreRecorded(st *state.State) {
	if st.Server != "" {
		_ = routing.RestoreByServerIP(st.Server)
	}
	if st.DNSApplied && st.DNSService != "" {
		_ = dnsmgr.FromRecorded(st.DNSService, st.DNSServers, true).Restore()
	}
}

// processAlive reports whether pid names a live process this user can
// signal — sending signal 0 performs the existence/permission check
// without actually delivering anything (man 2 kill).
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func resolveServer(host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return ip, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4, nil
		}
	}
	return ips[0], nil
}

func localOutboundIP(iface string) net.IP {
	out, err := exec.Command(sysbin.Ifconfig, iface).Output()
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return net.ParseIP(fields[1])
			}
		}
	}
	return nil
}
