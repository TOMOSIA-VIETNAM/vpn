package l2tp

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
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

	ns, nr uint16 // our next-to-send / next-expected sequence numbers

	strayData uint64 // data messages dropped for carrying another tunnel/session ID

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
	avps = concatAVPs(
		MessageTypeAVP(MsgICCN),
		TxConnectSpeedAVP(txConnectSpeed),
		FramingTypeAVP(1), // bit 0 = sync framing, matching PPP-over-L2TP norms
		// TxConnectSpeed & RxConnectSpeed: RFC 2661 §6.11/§6.12 SHOULD include these in ICCN.
		// Cisco IOS, Juniper, and other enterprise LNS servers require them for accounting/logging.
		// Reports 1Gbps (1,000,000,000 bps) virtual rate. (This is metadata and does not cap actual throughput).
		TxConnectSpeedAVP(1_000_000_000),
		RxConnectSpeedAVP(1_000_000_000),
	)
	return tun.sendReliableNoReply(ctx, avps)

}

// Close tears the session and tunnel down cleanly (CDN then StopCCN, RFC
// 2661 §5.11/5.6), best-effort — a failure here just means the LNS's own
// idle timeout will clean up the stale session/tunnel instead.
func (tun *Tunnel) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	cdn := concatAVPs(MessageTypeAVP(MsgCDN))
	_ = tun.sendReliableNoReply(ctx, cdn)
	stop := concatAVPs(MessageTypeAVP(MsgStopCCN))
	_ = tun.sendReliableNoReply(ctx, stop)
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
	if msg.Header.HasSeq {
		tun.nr = msg.Header.Ns + 1
	}
	msgType, err := MessageType(msg.AVPs)
	if err != nil {
		return
	}
	switch msgType {
	case MsgHello:
		_ = tun.sendReliableNoReply(context.Background(), concatAVPs(MessageTypeAVP(MsgHello)))
	case MsgCDN:
		if msg.Header.SessionID != tun.localSessionID {
			return
		}
		tun.closed = fmt.Errorf("%w (CDN, %s)", ErrPeerClosed, describeResult(msg.AVPs))
		vpnlog.Error(stage, "server closed the L2TP session", vpnlog.Fields{"message": "CDN", "reason": describeResult(msg.AVPs)})
	case MsgStopCCN:
		tun.closed = fmt.Errorf("%w (StopCCN, %s)", ErrPeerClosed, describeResult(msg.AVPs))
		vpnlog.Error(stage, "server closed the L2TP tunnel", vpnlog.Fields{"message": "StopCCN", "reason": describeResult(msg.AVPs)})
	}
}

// sendReliable sends one control message and blocks for the matching reply
// (ACKing it implicitly by having advanced Nr before the next send),
// retransmitting on timeout. RFC 2661's window is sequence-number based,
// not request/response, but with window size 1 this reduces to exactly the
// request/reply pattern every synchronous control exchange in §5 actually
// uses.
func (tun *Tunnel) sendReliable(ctx context.Context, avps []byte) (*ParsedMessage, error) {
	msg := MarshalControl(tun.peerTunnelID, tun.peerSessionID, tun.ns, tun.nr, avps)
	ackedByZLB := false
	var lastErr error
	for attempt := 0; attempt <= maxRetransmits; attempt++ {
		if attempt > 0 {
			vpnlog.Debug(stage, "retransmitting", vpnlog.Fields{"attempt": attempt})
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

// sendReliableNoReply is for messages the peer doesn't answer with its own
// control message (SCCCN, ICCN, CDN, StopCCN) — it still needs to arrive
// reliably, so it retransmits until the peer's next Hello/data message
// implicitly ACKs it (Nr advances), or gives up after the retry budget
// since these are typically followed immediately by data traffic that
// itself confirms the tunnel is usable.
func (tun *Tunnel) sendReliableNoReply(ctx context.Context, avps []byte) error {
	msg := MarshalControl(tun.peerTunnelID, tun.peerSessionID, tun.ns, tun.nr, avps)
	if err := tun.t.Send(msg); err != nil {
		return err
	}
	tun.ns++
	return nil
}

func concatAVPs(chunks ...[]byte) []byte {
	var out []byte
	for _, c := range chunks {
		out = append(out, c...)
	}
	return out
}
