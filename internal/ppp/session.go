package ppp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"vpn/internal/vpnlog"
)

const stage = "PPP"

// Transport is the minimum a PPP session needs from whatever carries its
// frames — the L2TP data channel, in this client. Kept minimal so the PPP
// state machine has no dependency on L2TP/UDP details.
type Transport interface {
	SendFrame(protocol uint16, payload []byte) error
	// RecvFrame blocks until a frame arrives or ctx is done.
	RecvFrame(ctx context.Context) (protocol uint16, payload []byte, err error)
}

// Config is what the engine supplies to bring up one PPP session.
type Config struct {
	MRU         uint16
	Username    string
	Password    string
	RequestedIP net.IP
	Timeout     time.Duration
}

// Result is everything the engine needs once PPP reaches the OPENED state.
type Result struct {
	IPCP NegotiatedIPCP
	// Magic is our LCP Magic-Number as negotiated (0 if the peer rejected
	// it), for answering the peer's Echo-Requests (see HandleOpenedLCP).
	Magic uint32
}

// Run drives DEAD -> ESTABLISH (LCP) -> AUTHENTICATE (MS-CHAPv2) ->
// NETWORK (IPCP) -> OPENED, per RFC 1661 §3's phase diagram. It returns
// once the link is fully usable, or an error identifying which phase
// failed (LCP_FAILED / PPP_AUTH_FAILURE / IPCP_FAILURE, matching the
// goal's diagnostic vocabulary).
func Run(ctx context.Context, t Transport, cfg Config) (*Result, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	lcp, err := NewLCPConfig(cfg.MRU)
	if err != nil {
		return nil, err
	}
	magic, peerAuth, err := runLCP(ctx, t, lcp)
	if err != nil {
		return nil, fmt.Errorf("LCP_FAILED: %w", err)
	}
	vpnlog.Info(stage, "LCP established", vpnlog.Fields{"mru": cfg.MRU, "auth_proto": peerAuth.Protocol, "auth_algo": peerAuth.Algorithm})

	if err := runAuth(ctx, t, cfg.Username, cfg.Password, magic, peerAuth); err != nil {
		return nil, fmt.Errorf("PPP_AUTH_FAILURE: %w", err)
	}
	vpnlog.Info(stage, "PPP authentication succeeded", nil)

	ipcp, err := runIPCP(ctx, t, cfg.RequestedIP)
	if err != nil {
		return nil, fmt.Errorf("IPCP_FAILURE: %w", err)
	}
	vpnlog.Info(stage, "IPCP established", vpnlog.Fields{"local_ip": ipcp.LocalIP.String()})

	return &Result{IPCP: *ipcp, Magic: magic}, nil
}

// negotiatePhase runs one side-agnostic RFC 1661 §4 option-negotiation loop
// for a given protocol (LCP or IPCP): send our Configure-Request, and
// process whatever the peer sends (their Configure-Request, our
// Ack/Nak/Reject, their Ack for ours) until both directions are
// acknowledged or ctx expires. buildRequest/handlePeerRequest let LCP and
// IPCP share this loop despite having different option semantics.
// negotiatePhase's return value is the final Configure-Request options *we*
// ended up sending once ourAcked — i.e. our own side of the negotiation
// (our address, in IPCP's case), as opposed to whatever the peer asked for
// itself via its own Configure-Request.
func negotiatePhase(
	ctx context.Context,
	t Transport,
	protocol uint16,
	buildRequestOptions func() []Option,
	handlePeerConfigureRequest func(opts []Option) (ackOpts []Option, ok bool),
) ([]Option, error) {
	var ourID uint8 = 1
	ourAcked := false
	peerAcked := false
	ourOptions := buildRequestOptions()

	send := func() error {
		pkt := ControlPacket{Code: CodeConfigureRequest, Identifier: ourID, Data: MarshalOptions(ourOptions)}
		return t.SendFrame(protocol, pkt.Marshal())
	}
	if err := send(); err != nil {
		return nil, err
	}

	retransmit := time.NewTicker(800 * time.Millisecond)
	defer retransmit.Stop()

	for !ourAcked || !peerAcked {
		frameCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
		proto, payload, err := t.RecvFrame(frameCtx)
		cancel()
		if err != nil {
			if !isWaitTimeout(err) {
				return nil, err // the link itself is gone (e.g. the LNS closed the session): retransmitting cannot help
			}
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("timed out waiting for peer (our_acked=%v peer_acked=%v)", ourAcked, peerAcked)
			default:
			}
			if err := send(); err != nil { // retransmit our request
				return nil, err
			}
			continue
		}
		if proto != protocol {
			continue // not this phase's protocol — ignore (e.g. stray LCP Echo during IPCP)
		}
		pkt, err := ParseControlPacket(payload)
		if err != nil {
			return nil, err
		}
		switch pkt.Code {
		case CodeConfigureRequest:
			opts, err := ParseOptions(pkt.Data)
			if err != nil {
				return nil, err
			}
			ackOpts, ok := handlePeerConfigureRequest(opts)
			code := uint8(CodeConfigureAck)
			respData := MarshalOptions(ackOpts)
			if !ok {
				code = CodeConfigureReject
				respData = pkt.Data
			}
			resp := ControlPacket{Code: code, Identifier: pkt.Identifier, Data: respData}
			if err := t.SendFrame(protocol, resp.Marshal()); err != nil {
				return nil, err
			}
			if ok {
				peerAcked = true
			}
		case CodeConfigureAck:
			if pkt.Identifier == ourID {
				ourAcked = true
			}
		case CodeConfigureNak:
			// RFC 1661 §5.4: a Nak counters specific option *values* — we
			// must adopt the peer's suggestions (e.g. its proposed IP/DNS
			// addresses in place of our 0.0.0.0 placeholders) before
			// resending, or the peer keeps Naking the same unacceptable
			// values forever.
			suggested, err := ParseOptions(pkt.Data)
			if err != nil {
				return nil, err
			}
			ourOptions = applyNak(ourOptions, suggested)
			ourID++
			if err := send(); err != nil {
				return nil, err
			}
		case CodeConfigureReject:
			// RFC 1661 §5.5: a Reject means the peer doesn't recognize the
			// option at all — drop it entirely (safe for every option this
			// client sends) rather than resending it unchanged.
			rejected, err := ParseOptions(pkt.Data)
			if err != nil {
				return nil, err
			}
			ourOptions = dropRejected(ourOptions, rejected)
			ourID++
			if err := send(); err != nil {
				return nil, err
			}
		case CodeEchoRequest:
			// LCP keepalive from a peer already Opened on its side — reply
			// so it doesn't tear down the link thinking we vanished.
			_ = t.SendFrame(protocol, EchoReply(pkt, magicOf(ourOptions)).Marshal())
		}
	}
	return ourOptions, nil
}

// applyNak replaces each of our options with the peer's suggested value for
// that same option type (RFC 1661 §5.4) — the peer only Naks options it
// wants changed, so any option type it didn't mention keeps our value.
func applyNak(ours []Option, suggested []Option) []Option {
	out := make([]Option, len(ours))
	copy(out, ours)
	for _, s := range suggested {
		for i, o := range out {
			if o.Type == s.Type {
				out[i] = s
				break
			}
		}
	}
	return out
}

// dropRejected removes every option the peer doesn't recognize at all (RFC
// 1661 §5.5) so the next Configure-Request doesn't offer it again.
func dropRejected(ours []Option, rejected []Option) []Option {
	out := make([]Option, 0, len(ours))
	for _, o := range ours {
		skip := false
		for _, r := range rejected {
			if r.Type == o.Type {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, o)
		}
	}
	return out
}

// runLCP returns our Magic-Number as the peer finally acked it (0 if it
// rejected the option) and the peer's negotiated AuthProtocol.
func runLCP(ctx context.Context, t Transport, cfg LCPConfig) (uint32, AuthProtocol, error) {
	var peerAuth AuthProtocol
	ourOptions, err := negotiatePhase(ctx, t, ProtoLCP,
		cfg.ConfigureRequestOptions,
		func(opts []Option) ([]Option, bool) {
			for _, o := range opts {
				if o.Type == OptAuthProtocol {
					if ap, err := ParseAuthProtocolOption(o.Data); err == nil {
						peerAuth = ap
					}
				}
			}
			return opts, true
		})
	if err != nil {
		return 0, AuthProtocol{}, err
	}
	return magicOf(ourOptions), peerAuth, nil
}

func runIPCP(ctx context.Context, t Transport, requestedIP net.IP) (*NegotiatedIPCP, error) {
	result := &NegotiatedIPCP{}
	// handlePeerConfigureRequest ACKs the LNS's own Configure-Request
	// unmodified, and separately records its IP-Address option as PeerIP
	// (the LNS's inside address, needed for the utun point-to-point setup)
	// — distinct from our own address/DNS, which come back as the Ack to
	// *our* Configure-Request instead, so the result is built from
	// negotiatePhase's returned ourOptions below, not from the peer's
	// Configure-Request.
	ourOptions, err := negotiatePhase(ctx, t, ProtoIPCP,
		func() []Option { return RequestIPCPOptions(requestedIP) },
		func(opts []Option) ([]Option, bool) {
			for _, o := range opts {
				result.ApplyPeerOption(o)
			}
			return opts, true
		})
	if err != nil {
		return nil, err
	}
	for _, o := range ourOptions {
		result.ApplyOption(o)
	}
	if result.LocalIP == nil {
		return nil, fmt.Errorf("IPCP completed without an assigned IP address")
	}
	return result, nil
}

// Terminate sends an LCP Terminate-Request and waits briefly for the peer's
// Terminate-Ack (RFC 1661 §5.6), best-effort — a failure here just means the
// LNS's own idle timeout will clean up the link instead. This matters beyond
// tidiness: an LNS that never sees a Terminate-Request has no clean signal
// that this PPP session (and the RADIUS/AAA login behind it) actually ended,
// which can make it treat a same-user reconnect moments later as a
// still-active duplicate session and reject the new MS-CHAPv2 auth. Skipping
// straight to tearing down L2TP/IKE — as this client used to do — reproduced
// exactly that: disconnect immediately followed by connect failed with
// PPP_AUTH_FAILURE even though the credentials never changed.
func Terminate(t Transport, id uint8) {
	// Measured live against the LNS: the Ack arrives 170-210 ms after the
	// Request, so 250 ms missed about a third of them. It returns as soon as
	// the Ack is in, so the longer bound only costs time when none comes.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req := ControlPacket{Code: CodeTerminateRequest, Identifier: id}
	start := time.Now()
	if err := t.SendFrame(ProtoLCP, req.Marshal()); err != nil {
		vpnlog.Debug(stage, "could not send Terminate-Request", vpnlog.Fields{"err": err})
		return
	}
	for {
		proto, payload, err := t.RecvFrame(ctx)
		if err != nil {
			vpnlog.Debug(stage, "no Terminate-Ack", vpnlog.Fields{"err": err, "waited_ms": time.Since(start).Milliseconds()})
			return
		}
		if proto != ProtoLCP {
			continue
		}
		pkt, err := ParseControlPacket(payload)
		if err != nil {
			continue
		}
		if pkt.Code == CodeTerminateAck {
			vpnlog.Debug(stage, "Terminate-Ack received", vpnlog.Fields{"waited_ms": time.Since(start).Milliseconds()})
			return
		}
	}
}

func runAuth(ctx context.Context, t Transport, username, password string, magic uint32, peerAuth AuthProtocol) error {
	if peerAuth.Protocol == ProtoPAP {
		return runPAPAuth(ctx, t, username, password, magic)
	}

	// Every Response sent so far. A retransmitted Challenge gets a fresh
	// Response (new PeerChallenge), but the LNS may still answer an earlier
	// one — e.g. pppd resends the Success it already computed for Response
	// #1 when a late Response #2 arrives — so Success is valid if it
	// authenticates any Response we actually sent.
	var sent []*MSCHAPv2Response
	var sentMD5 bool
	for {
		frameCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		proto, payload, err := t.RecvFrame(frameCtx)
		cancel()
		if err != nil {
			if !isWaitTimeout(err) {
				return err
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("timed out waiting for authentication challenge")
			default:
				continue
			}
		}
		if proto == ProtoLCP {
			// The peer can keep retransmitting its own LCP Configure-Request
			// here if the Ack negotiatePhase sent it back during the LCP
			// phase never arrived — negotiatePhase itself has already
			// exited by now, so nothing else acks it. Left unanswered, the
			// peer's own LCP state machine can stay stuck waiting for that
			// Ack and never send a CHAP Challenge at all — reproduced live
			// as an endless "timed out waiting for CHAP Challenge" while
			// the log showed the peer's identical Configure-Request on
			// repeat. Keep acking it here too so the peer's LCP actually
			// converges — and answer its Echo-Requests, which start as soon
			// as its LCP is Opened.
			HandleOpenedLCP(t, payload, magic)
			continue
		}
		if proto == ProtoPAP {
			return runPAPAuth(ctx, t, username, password, magic)
		}
		if proto != ProtoCHAP {
			continue
		}
		pkt, err := ParseCHAPPacket(payload)
		if err != nil {
			return err
		}
		switch pkt.Code {
		case CHAPCodeChallenge:
			if peerAuth.Algorithm == CHAPAlgoMD5 {
				sentMD5 = true
				resp := GenerateCHAPMD5Response(pkt.Identifier, password, pkt.Value)
				reply := CHAPPacket{Code: CHAPCodeResponse, Identifier: pkt.Identifier, Value: resp, Name: []byte(username)}
				if err := t.SendFrame(ProtoCHAP, reply.Marshal()); err != nil {
					return err
				}
			} else {
				resp, err := GenerateMSCHAPv2Response(pkt.Value, username, password)
				if err != nil {
					return err
				}
				sent = append(sent, resp)
				reply := CHAPPacket{Code: CHAPCodeResponse, Identifier: pkt.Identifier, Value: resp.Marshal(), Name: []byte(username)}
				if err := t.SendFrame(ProtoCHAP, reply.Marshal()); err != nil {
					return err
				}
			}
		case CHAPCodeSuccess:
			if sentMD5 {
				return nil
			}
			if len(sent) == 0 {
				return fmt.Errorf("CHAP Success received before any Challenge was answered")
			}
			var verifyErr error
			for _, r := range sent {
				if verifyErr = r.VerifySuccess(pkt.Message); verifyErr == nil {
					return nil
				}
			}
			return verifyErr
		case CHAPCodeFailure:
			return fmt.Errorf("CHAP authentication rejected by peer: %s", string(pkt.Message))
		}
	}
}

func runPAPAuth(ctx context.Context, t Transport, username, password string, magic uint32) error {
	req := GeneratePAPAuthenticateRequest(1, username, password)
	if err := t.SendFrame(ProtoPAP, req.Marshal()); err != nil {
		return err
	}
	for {
		frameCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		proto, payload, err := t.RecvFrame(frameCtx)
		cancel()
		if err != nil {
			select {
			case <-ctx.Done():
				return fmt.Errorf("timed out waiting for PAP response")
			default:
				_ = t.SendFrame(ProtoPAP, req.Marshal())
				continue
			}
		}
		if proto == ProtoLCP {
			HandleOpenedLCP(t, payload, magic)
			continue
		}
		if proto != ProtoPAP {
			continue
		}
		pkt, err := ParsePAPPacket(payload)
		if err != nil {
			return err
		}
		switch pkt.Code {
		case PAPCodeAuthenticateAck:
			return nil
		case PAPCodeAuthenticateNak:
			msg := ParsePAPMessage(pkt.Data)
			return fmt.Errorf("PAP authentication rejected by peer: %s", msg)
		}
	}
}

// TerminateNoWait sends the LCP Terminate-Request and returns at once. Used
// when the peer has already stopped answering: waiting the full Terminate
// timeout for an Ack that cannot come only delays the reconnect.
func TerminateNoWait(t Transport, id uint8) {
	req := ControlPacket{Code: CodeTerminateRequest, Identifier: id}
	_ = t.SendFrame(ProtoLCP, req.Marshal())
}

// isWaitTimeout tells a RecvFrame that merely ran out of time (keep waiting,
// or give up on the phase deadline) from one whose transport failed for good.
func isWaitTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}
