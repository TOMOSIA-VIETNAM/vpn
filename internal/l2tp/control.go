package l2tp

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"vpn/internal/vpnlog"
)

const stage = "L2TP"

// txConnectSpeed is what ICCN reports as the call's (Tx) Connect Speed:
// xl2tpd's default, 10 Mbit/s. The value is informational for an L2TP/IPsec
// client, but the AVP itself is mandatory.
const txConnectSpeed = 10000000

// Transport is the minimum a control connection needs from whatever carries
// its datagrams — the ESP-protected UDP/1701 socket, in this client.
type Transport interface {
	Send([]byte) error
	Recv(ctx context.Context) ([]byte, error)
}

const (
	retransmitInterval = 800 * time.Millisecond
	maxRetransmits     = 5
)

// Tunnel is one established L2TP control connection: reliable, sequenced
// control-message delivery (RFC 2661 §5.8's sliding window, simplified to
// window size 1 — this client only ever has one message in flight, which
// the reference LNS's own default receive window comfortably accepts) plus
// the tunnel/session IDs needed to build the data channel.
type Tunnel struct {
	t Transport

	localTunnelID  uint16
	peerTunnelID   uint16
	localSessionID uint16
	peerSessionID  uint16

	ctrlMu sync.Mutex
	ns, nr uint16 // our next-to-send / next-expected sequence numbers

	// The control message we sent that the peer has not acknowledged yet
	// (window size 1, RFC 2661 §5.8). It must reach the peer before anything
	// numbered after it: an LNS such as xl2tpd drops every control message
	// whose Ns is ahead of the one it expects — ZLB acks included — without
	// reading its Nr. If this message were lost and never resent, the server
	// would drop all our acks until its own Hello timed out (StopCCN
	// "Timeout", 31 s later). So until Nr from the peer passes pendingNs,
	// this message is resent in place of every ack and every new Hello.
	pending    []byte // AVPs
	pendingNs  uint16
	hasPending bool

	// Data messages of our session that arrived while Establish was waiting
	// for an ack (awaitAckLocked) — typically the LNS's first LCP
	// Configure-Request right after ICCN. RecvData returns them first.
	early [][]byte

	strayData uint64 // data messages dropped for carrying another tunnel/session ID
	resends   uint64 // times the current pending message was sent again (logged, see resendPendingLocked)

	closed error // set once the LNS ends our session or tunnel (CDN/StopCCN); RecvData returns it
}

// ErrPeerClosed is wrapped when the LNS ends our session (CDN) or tunnel
// (StopCCN), so the layer above stops waiting on a link that is gone.
var ErrPeerClosed = errors.New("the server closed the L2TP session")

// Config is what the engine supplies to establish one tunnel+session.
type Config struct {
	HostName string
	Timeout  time.Duration
}

// randomTunnelID picks a fresh local Tunnel ID per connection attempt
// instead of a fixed value. A hard-coded ID collides with a stale tunnel
// the LNS may still be holding under that same ID from a previous run (the
// server then ACKs our SCCRQ with a ZLB but never completes SCCRP because
// it's confused about which control connection we mean) — RFC 2661 doesn't
// require any particular ID, so a random one sidesteps that collision.
func randomTunnelID() uint16 {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 1
	}
	id := binary.BigEndian.Uint16(b[:])
	if id == 0 {
		id = 1
	}
	return id
}

// Establish runs SCCRQ/SCCRP/SCCCN (control connection) followed by
// ICRQ/ICRP/ICCN (incoming call/session) — RFC 2661 §5.1-5.4 — leaving the
// data channel ready for PPP frames.
func Establish(ctx context.Context, t Transport, cfg Config) (*Tunnel, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	tun := &Tunnel{t: t, localTunnelID: randomTunnelID()}

	if err := tun.doSCC(ctx, cfg.HostName); err != nil {
		return nil, fmt.Errorf("L2TP_TIMEOUT: control connection (SCCRQ/SCCRP/SCCCN): %w", err)
	}
	vpnlog.Info(stage, "control connection established", vpnlog.Fields{"local_tunnel_id": tun.localTunnelID, "peer_tunnel_id": tun.peerTunnelID})

	if err := tun.doIncomingCall(ctx); err != nil {
		return nil, fmt.Errorf("L2TP_TIMEOUT: session setup (ICRQ/ICRP/ICCN): %w", err)
	}
	vpnlog.Info(stage, "session established", vpnlog.Fields{"local_session_id": tun.localSessionID, "peer_session_id": tun.peerSessionID})

	return tun, nil
}

// ackWait bounds how long Establish waits for the peer to acknowledge a
// message that has no reply of its own (SCCCN, ICCN), resending it every
// retransmitInterval. It is best effort: when it runs out the message stays
// pending and is resent with the next ack or Hello.
var ackWait = 3 * time.Second

func (tun *Tunnel) doSCC(ctx context.Context, hostName string) error {
	avps := concatAVPs(
		MessageTypeAVP(MsgSCCRQ),
		ProtocolVersionAVP(),
		HostNameAVP(hostName),
		FramingCapAVP(),
		BearerCapAVP(), // RFC 2661 §5.1: mandatory in SCCRQ, not just recommended
		AssignedTunnelIDAVP(tun.localTunnelID),
		ReceiveWindowSizeAVP(1),
	)
	resp, err := tun.sendReliable(ctx, avps)
	if err != nil {
		return err
	}
	msgType, err := MessageType(resp.AVPs)
	if err != nil {
		vpnlog.Debug(stage, "SCCRQ reply had no Message Type", vpnlog.Fields{
			"header": fmt.Sprintf("%+v", resp.Header), "num_avps": len(resp.AVPs),
		})
		for _, a := range resp.AVPs {
			vpnlog.Debug(stage, "  avp", vpnlog.Fields{"vendor": a.VendorID, "type": a.Type, "hex": fmt.Sprintf("%x", a.Value)})
		}
		return err
	}
	if msgType != MsgSCCRP {
		return fmt.Errorf("expected SCCRP, got message type %d", msgType)
	}
	if avp, ok := Find(resp.AVPs, AVPAssignedTunnelID); ok && len(avp.Value) >= 2 {
		tun.peerTunnelID = binary.BigEndian.Uint16(avp.Value)
	} else {
		return fmt.Errorf("SCCRP missing Assigned Tunnel ID")
	}

	// SCCCN stays pending; ICRQ (sendReliable) waits for its ack first.
	avps = concatAVPs(MessageTypeAVP(MsgSCCCN))
	return tun.sendReliableNoReply(ctx, avps)
}

func (tun *Tunnel) doIncomingCall(ctx context.Context) error {
	tun.localSessionID = 1
	avps := concatAVPs(
		MessageTypeAVP(MsgICRQ),
		AssignedSessionIDAVP(tun.localSessionID),
		CallSerialNumberAVP(1),
		BearerTypeAVP(0), // optional here (RFC 2661 §6.10); 0 = digital — this client has no analog modem
	)
	resp, err := tun.sendReliable(ctx, avps)
	if err != nil {
		return err
	}
	msgType, err := MessageType(resp.AVPs)
	if err != nil {
		return err
	}
	if msgType != MsgICRP {
		return fmt.Errorf("expected ICRP, got message type %d", msgType)
	}
	if avp, ok := Find(resp.AVPs, AVPAssignedSessionID); ok && len(avp.Value) >= 2 {
		tun.peerSessionID = binary.BigEndian.Uint16(avp.Value)
	} else {
		return fmt.Errorf("ICRP missing Assigned Session ID")
	}

	// RFC 2661 §6.12: ICCN must carry (Tx) Connect Speed and Framing Type,
	// and Bearer Type has no place in it. xl2tpd lets either slide; a stricter
	// LNS treats the message as malformed and never starts PPP for the
	// session — seen live as LCP getting no answer at all.
	// TxConnectSpeed & RxConnectSpeed: RFC 2661 §6.11/§6.12 SHOULD include these in ICCN.
	// Cisco IOS, Juniper, and other enterprise LNS servers require them for accounting/logging.
	// Reports 1Gbps (1,000,000,000 bps) virtual rate. (This is metadata and does not cap actual throughput).
	avps = concatAVPs(
		MessageTypeAVP(MsgICCN),
		FramingTypeAVP(1), // bit 0 = sync framing, matching PPP-over-L2TP norms
		TxConnectSpeedAVP(1_000_000_000),
		RxConnectSpeedAVP(1_000_000_000),
	)
	if err := tun.sendReliableNoReply(ctx, avps); err != nil {
		return err
	}
	// A lost ICCN means the LNS never starts PPP for the session (seen live
	// as LCP timing out with no answer at all). Wait for its ack here.
	tun.ctrlMu.Lock()
	defer tun.ctrlMu.Unlock()
	tun.awaitAckLocked(ctx)
	return nil
}

// SendHello sends an explicit L2TP Hello keepalive message (RFC 2661 §5.8).
// When received by the LNS, it will answer with a ZLB (or Hello) acknowledging
// our sequence number, keeping the ESP transport alive and resetting the
// liveness watchdog even if the PPP layer has disabled/unsupported LCP echoes.
//
// While an earlier message is still unacknowledged, that message is resent
// instead: a new Hello would carry an Ns the peer drops as out of order.
func (tun *Tunnel) SendHello() error {
	tun.ctrlMu.Lock()
	defer tun.ctrlMu.Unlock()
	if tun.hasPending {
		return tun.resendPendingLocked()
	}
	return tun.queueLocked(concatAVPs(MessageTypeAVP(MsgHello)))
}

// Unacked reports whether a control message of ours is still waiting for
// the peer's acknowledgment (for tests and diagnostics).
func (tun *Tunnel) Unacked() bool {
	tun.ctrlMu.Lock()
	defer tun.ctrlMu.Unlock()
	return tun.hasPending
}

// Close tears the session and tunnel down cleanly (CDN then StopCCN, RFC
// 2661 §5.11/5.6), best-effort — a failure here just means the LNS's own
// idle timeout will clean up the stale session/tunnel instead.
func (tun *Tunnel) Close() {
	tun.ctrlMu.Lock()
	defer tun.ctrlMu.Unlock()
	if tun.hasPending {
		// CDN is numbered after it: send it first so that, if this copy
		// arrives, the CDN right behind it is in order and accepted.
		_ = tun.resendPendingLocked()
	}
	// Nothing resends after Close, so CDN and StopCCN are sent once, back to
	// back, and not tracked as pending.
	_ = tun.sendOnceLocked(concatAVPs(MessageTypeAVP(MsgCDN)))
	_ = tun.sendOnceLocked(concatAVPs(MessageTypeAVP(MsgStopCCN)))
}

// sendOnceLocked sends a numbered control message without keeping it for
// retransmission (teardown only).
func (tun *Tunnel) sendOnceLocked(avps []byte) error {
	msg := MarshalControl(tun.peerTunnelID, tun.peerSessionID, tun.ns, tun.nr, avps)
	tun.ns++
	return tun.t.Send(msg)
}

// PeerIDs are the tunnel and session IDs the LNS assigned to this connection —
// what it expects in the header of every message we send it.
func (tun *Tunnel) PeerIDs() (tunnelID, sessionID uint16) {
	return tun.peerTunnelID, tun.peerSessionID
}

// Transport returns the underlying data plane transport.
func (tun *Tunnel) Transport() Transport {
	return tun.t
}

// SendDataTo sends one PPP frame in an L2TP data message addressed to an
// arbitrary tunnel/session of the LNS — used to end a session of ours whose
// Tunnel object no longer exists (see the engine's eviction of a stale
// session). Data messages carry no sequence numbers, so no control-channel
// state is needed to send one.
func SendDataTo(t Transport, peerTunnelID, peerSessionID uint16, pppFrame []byte) error {
	return t.Send(MarshalData(peerTunnelID, peerSessionID, pppFrame))
}

// SendData wraps one PPP frame in an L2TP data message (unsequenced, RFC
// 2661 §5.7.2 — this client relies on ESP + PPP's own LCP/CHAP retries for
// reliability rather than L2TP data sequencing, matching the reference
// xl2tpd's default behavior).
func (tun *Tunnel) SendData(pppFrame []byte) error {
	return tun.t.Send(MarshalData(tun.peerTunnelID, tun.peerSessionID, pppFrame))
}

// RecvData blocks for the next data message and returns its PPP frame,
// silently ACKing (and discarding) any control message that arrives
// interleaved with data (e.g. a Hello keepalive from the LNS).
func (tun *Tunnel) RecvData(ctx context.Context) ([]byte, error) {
	for {
		if tun.closed != nil {
			return nil, tun.closed
		}
		if len(tun.early) > 0 {
			frame := tun.early[0]
			tun.early = tun.early[1:]
			return frame, nil
		}
		raw, err := tun.t.Recv(ctx)
		if err != nil {
			return nil, err
		}
		msg, err := Parse(raw)
		if err != nil {
			continue
		}
		if !msg.Header.IsControl {
			// Data for another tunnel/session is not ours. The server keeps
			// sending a stale session's traffic (IP, LCP echo, even a
			// Terminate-Request) to the newest IPsec SA of this address, so
			// after a reconnect it arrives here mixed into the new session —
			// where it would be injected into the tunnel as if it were ours,
			// or stall PPP negotiation. The IDs in a received header are the
			// ones this side assigned (RFC 2661 §3.1).
			if msg.Header.TunnelID != tun.localTunnelID || msg.Header.SessionID != tun.localSessionID {
				tun.strayData++
				if tun.strayData == 1 || tun.strayData%500 == 0 {
					vpnlog.Info(stage, "ignored data for another L2TP session (stale session on the server?)", vpnlog.Fields{
						"tunnel": msg.Header.TunnelID, "session": msg.Header.SessionID, "dropped_total": tun.strayData,
					})
				}
				continue
			}
			return msg.Payload, nil
		}
		tun.handleInterleavedControl(msg)
	}
}

func (tun *Tunnel) handleInterleavedControl(msg *ParsedMessage) {
	if msg.Header.TunnelID != tun.localTunnelID {
		return // not addressed to our tunnel — ignore (e.g. another tunnel's Hello)
	}
	tun.ctrlMu.Lock()
	if msg.Header.HasSeq {
		tun.ackedLocked(msg.Header.Nr) // every control message, ZLB or not, acknowledges ours up to Nr
	}
	if len(msg.AVPs) == 0 {
		tun.ctrlMu.Unlock()
		return // a ZLB: a pure acknowledgment of ours, taking no sequence number (RFC 2661 §5.8)
	}
	if msg.Header.HasSeq {
		switch {
		case msg.Header.Ns == tun.nr:
			tun.nr++
		case seqBefore(msg.Header.Ns, tun.nr):
			tun.ackPeerLocked() // a retransmit: our acknowledgment was lost — ack again, act only once
			tun.ctrlMu.Unlock()
			return
		default:
			tun.ctrlMu.Unlock()
			return // ahead of the one we expect (window 1): the peer resends it in order
		}
	}
	// Every control message is acknowledged (RFC 2661 §5.8). Only Hello used to
	// be — by a Hello of our own — so an LNS sending anything else (Set-Link-
	// Info once PPP is up, say) got no ack, retransmitted, and then closed the
	// tunnel: seen live as StopCCN "result 7" (state machine error) ~40 s in.
	tun.ackPeerLocked()
	tun.ctrlMu.Unlock()
	msgType, err := MessageType(msg.AVPs)
	if err != nil {
		return
	}
	switch msgType {
	case MsgHello: // the ZLB above answers it
	case MsgCDN:
		if msg.Header.SessionID != tun.localSessionID {
			return
		}
		tun.closed = fmt.Errorf("%w (CDN, %s)", ErrPeerClosed, describeResult(msg.AVPs))
		vpnlog.Error(stage, "server closed the L2TP session", vpnlog.Fields{"message": "CDN", "reason": describeResult(msg.AVPs)})
	case MsgStopCCN:
		tun.closed = fmt.Errorf("%w (StopCCN, %s)", ErrPeerClosed, describeResult(msg.AVPs))
		vpnlog.Error(stage, "server closed the L2TP tunnel", vpnlog.Fields{"message": "StopCCN", "reason": describeResult(msg.AVPs)})
	default:
		vpnlog.Info(stage, "server sent a control message — acknowledged", vpnlog.Fields{"type": msgType, "ns": msg.Header.Ns})
	}
}

// ackPeerLocked acknowledges everything received so far. With a message of
// ours still unacknowledged, a ZLB would carry an Ns past it, which the peer
// drops unread; resending that message carries the same Nr and fills the gap.
func (tun *Tunnel) ackPeerLocked() {
	if tun.hasPending {
		_ = tun.resendPendingLocked()
		return
	}
	tun.sendZLB()
}

// ackedLocked records the peer's Nr: it has received every message of ours
// numbered below it.
func (tun *Tunnel) ackedLocked(nr uint16) {
	if tun.hasPending && seqBefore(tun.pendingNs, nr) {
		tun.hasPending = false
		tun.pending = nil
		tun.resends = 0
	}
}

// errUnacked is returned by queueLocked while an earlier message is still
// pending: with window 1 the new one cannot be sent yet.
var errUnacked = errors.New("an earlier control message is not acknowledged yet")

// queueLocked sends a message that gets no reply of its own and keeps it
// pending until the peer acknowledges it. It never replaces a message still
// pending (that one would be lost for good); callers check hasPending first.
func (tun *Tunnel) queueLocked(avps []byte) error {
	if tun.hasPending {
		return errUnacked
	}
	tun.pending, tun.pendingNs, tun.hasPending = avps, tun.ns, true
	tun.ns++
	return tun.sendPendingLocked()
}

// sendPendingLocked sends the pending message with our current Nr, as RFC
// 2661 §5.8 asks of a retransmission too.
func (tun *Tunnel) sendPendingLocked() error {
	return tun.t.Send(MarshalControl(tun.peerTunnelID, tun.peerSessionID, tun.pendingNs, tun.nr, tun.pending))
}

// resendPendingLocked sends the pending message again because the peer has
// not acknowledged it, and logs it,
// so a lost control message shows up in the log instead of a StopCCN.
func (tun *Tunnel) resendPendingLocked() error {
	tun.resends++
	if tun.resends == 1 || tun.resends%50 == 0 {
		var msgType uint16
		if avps, err := ParseAVPs(tun.pending); err == nil {
			msgType, _ = MessageType(avps)
		}
		vpnlog.Info(stage, "control message not acknowledged yet — resending it", vpnlog.Fields{"type": msgType, "ns": tun.pendingNs, "resends": tun.resends})
	}
	return tun.sendPendingLocked()
}

// maxEarly bounds the data frames kept while waiting for an ack: PPP
// retransmits anything beyond them.
const maxEarly = 8

// awaitAckLocked waits up to ackWait for the pending message to be
// acknowledged, resending it every retransmitInterval. Only used while the
// tunnel is being set up, when nothing else reads the transport.
//
// A data message of our session ends the wait: the LNS only starts PPP once
// it has our ICCN, and the frame is kept for RecvData instead of dropped. A
// new control message of the peer (Ns == nr) is left unacknowledged here; the
// peer retransmits it and RecvData then handles it.
func (tun *Tunnel) awaitAckLocked(ctx context.Context) {
	deadline := time.Now().Add(ackWait)
	for tun.hasPending && time.Now().Before(deadline) {
		wait := min(retransmitInterval, time.Until(deadline))
		recvCtx, cancel := context.WithTimeout(ctx, wait)
		raw, err := tun.t.Recv(recvCtx)
		timedOut := errors.Is(recvCtx.Err(), context.DeadlineExceeded)
		cancel()
		if err != nil {
			if ctx.Err() != nil || !timedOut {
				return // cancelled, or the transport failed: resending cannot help
			}
			_ = tun.resendPendingLocked()
			continue
		}
		m, err := Parse(raw)
		if err != nil || m.Header.TunnelID != tun.localTunnelID {
			continue
		}
		if !m.Header.IsControl {
			if m.Header.SessionID == tun.localSessionID && len(tun.early) < maxEarly {
				// Copied: the transport may reuse the buffer (ReleaseLast).
				tun.early = append(tun.early, append([]byte(nil), m.Payload...))
				return
			}
			continue
		}
		if !m.Header.HasSeq {
			continue
		}
		tun.ackedLocked(m.Header.Nr)
		if len(m.AVPs) > 0 && seqBefore(m.Header.Ns, tun.nr) {
			tun.ackPeerLocked() // the peer resent its last message: our ack was lost too
		}
	}
	if tun.hasPending {
		vpnlog.Info(stage, "control message not acknowledged yet — it will be resent with the next ack or Hello", vpnlog.Fields{"ns": tun.pendingNs})
	}
}

// sendZLB acknowledges everything received so far (Nr) without sending
// anything else: a control header with no AVPs, which takes no sequence
// number of its own (RFC 2661 §5.8).
func (tun *Tunnel) sendZLB() {
	_ = tun.t.Send(MarshalControl(tun.peerTunnelID, 0, tun.ns, tun.nr, nil))
}

// seqBefore reports whether sequence number a precedes b, modulo 2^16 (RFC 2661 §5.8).
func seqBefore(a, b uint16) bool { return int16(a-b) < 0 }

// sendReliable sends one control message and blocks for the matching reply
// (ACKing it implicitly by having advanced Nr before the next send),
// retransmitting on timeout. RFC 2661's window is sequence-number based,
// not request/response, but with window size 1 this reduces to exactly the
// request/reply pattern every synchronous control exchange in §5 actually
// uses.
func (tun *Tunnel) sendReliable(ctx context.Context, avps []byte) (*ParsedMessage, error) {
	tun.ctrlMu.Lock()
	defer tun.ctrlMu.Unlock()
	if tun.hasPending {
		// Window 1: the previous message (SCCCN before ICRQ) must arrive
		// first, or the peer drops this one as out of order.
		tun.awaitAckLocked(ctx)
		if tun.hasPending {
			_ = tun.resendPendingLocked() // last try; the reply to this message acks it too
		}
	}
	msg := MarshalControl(tun.peerTunnelID, tun.peerSessionID, tun.ns, tun.nr, avps)
	ackedByZLB := false
	var lastErr error
	for attempt := 0; attempt <= maxRetransmits; attempt++ {
		if attempt > 0 {
			vpnlog.Debug(stage, "retransmitting", vpnlog.Fields{"attempt": attempt})
			if tun.hasPending {
				_ = tun.resendPendingLocked() // still unacknowledged: it must precede this message or the peer drops it again
			}
		}
		if err := tun.t.Send(msg); err != nil {
			return nil, err
		}
		deadline := time.Now().Add(retransmitInterval)
		for {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				lastErr = fmt.Errorf("no reply within %s", retransmitInterval)
				break
			}
			recvCtx, cancel := context.WithTimeout(ctx, remaining)
			raw, err := tun.t.Recv(recvCtx)
			cancel()
			if err != nil {
				lastErr = err
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
				}
				break
			}
			parsed, err := Parse(raw)
			if err != nil {
				lastErr = err
				vpnlog.Debug(stage, "unparsable control reply", vpnlog.Fields{"bytes": len(raw), "hex": fmt.Sprintf("%x", raw), "err": err})
				continue
			}
			if !parsed.Header.IsControl {
				continue // stray data message before the session exists yet — ignore
			}
			if parsed.Header.TunnelID != tun.localTunnelID {
				// Traffic for a different tunnel sharing the same NAT-T
				// mapping (e.g. a stale tunnel's Hello keepalive) — not a
				// reply to anything we sent, ignore it.
				vpnlog.Debug(stage, "ignoring control message for a different tunnel", vpnlog.Fields{
					"got_tunnel_id": parsed.Header.TunnelID, "our_tunnel_id": tun.localTunnelID,
				})
				continue
			}
			if parsed.Header.HasSeq {
				tun.ackedLocked(parsed.Header.Nr)
			}
			if parsed.Header.HasSeq && parsed.Header.Ns != tun.nr {
				continue // not the message we're waiting for (could be a retransmit or Hello)
			}
			if !ackedByZLB {
				tun.ns++ // this reply (ZLB or not) confirms the peer received our message
				ackedByZLB = true
			}
			if len(parsed.AVPs) == 0 {
				// RFC 2661 §5.5: a ZLB (Zero-Length Body) message is a pure
				// ACK with no AVPs and does not itself consume a sequence
				// number — the real SCCRP/ICRP that follows reuses the same
				// Ns, so this does *not* advance tun.nr (unlike a
				// substantive message below) and keeps listening within
				// this same attempt instead of retransmitting.
				vpnlog.Debug(stage, "received ZLB ack, still waiting for the real reply", nil)
				continue
			}
			if parsed.Header.HasSeq {
				tun.nr = parsed.Header.Ns + 1
			}
			return parsed, nil
		}
	}
	return nil, fmt.Errorf("no reply after %d attempts: %v", maxRetransmits+1, lastErr)
}

// sendReliableNoReply is for the setup messages the peer doesn't answer with
// its own control message (SCCCN, ICCN). The message stays pending until the
// peer's Nr acknowledges it and is resent meanwhile (see Tunnel.pending), so
// a single lost datagram does not desynchronize the control channel. Setup
// only: with an earlier message still pending it reads the transport itself
// (awaitAckLocked).
func (tun *Tunnel) sendReliableNoReply(ctx context.Context, avps []byte) error {
	tun.ctrlMu.Lock()
	defer tun.ctrlMu.Unlock()
	if tun.hasPending {
		tun.awaitAckLocked(ctx)
	}
	return tun.queueLocked(avps)
}

func concatAVPs(chunks ...[]byte) []byte {
	var out []byte
	for _, c := range chunks {
		out = append(out, c...)
	}
	return out
}
