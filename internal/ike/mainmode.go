package ike

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"

	"vpn/internal/vpnlog"
)

const stage = "IKE"

// retransmit tuning: an unanswered message is resent up to maxRetransmits
// times with a fixed backoff before the exchange gives up with a timeout —
// simple and bounded, matching how racoon/strongSwan behave by default
// (their defaults are a few seconds per retry, a handful of retries).
const maxRetransmits = 5

// retransmitInterval is a variable only so tests can shorten it.
var retransmitInterval = 3 * time.Second

// Session is an established Phase 1 (IKE) SA: everything Phase 2 (Quick
// Mode / ESP) needs to proceed, without re-deriving anything.
//
// The socket is deliberately *unconnected* (net.ListenUDP, not DialUDP).
// A connected UDP socket makes the kernel silently drop any datagram whose
// source port doesn't match the address it was "connected" to — which is
// exactly wrong here: a real server (confirmed live against the reference
// VPN server) keeps replying to Informational/Notify exchanges from port
// 500 even after Main Mode has floated the client to port 4500 for NAT-T.
// An unconnected socket lets this client accept any port from the server's
// IP, the way every real IKE daemon does, instead of missing half the
// conversation.
type Session struct {
	conn         *net.UDPConn
	serverIP     net.IP
	destAddr     *net.UDPAddr // where the next message should be sent — :500 until floated, then :4500
	InitiatorSPI [8]byte
	ResponderSPI [8]byte
	Keys         *Phase1Keys
	Transform    Transform
	NATDetected  bool
	LocalIP      net.IP
	// L2TPPort is our L2TP UDP port in every Quick Mode on this IKE SA (0 =
	// the standard 1701). Clients behind one NAT all look like the same
	// address to the server; with the same port too, the server cannot keep
	// their SAs apart and hands one client's L2TP traffic to another's SA.
	L2TPPort  uint16
	nextMsgID uint32
	lastIV    []byte // Main Mode's running CBC chain: last ciphertext block sent/received
	phase1IV  []byte // lastIV as Main Mode left it; seeds every later exchange's first IV

	// EstablishedAt and Lifetime describe the IKE SA itself (the responder's
	// chosen life duration). A Quick Mode rekey needs a live IKE SA.
	EstablishedAt time.Time
	Lifetime      time.Duration

	dp             *dataPlane // set by StartDataPhase; from then on the reader owns the socket
	mm1Retransmits int        // >0: MM1's retransmit budget, shortened when a fallback port follows
	closeOnce      sync.Once
	floated        bool // once true, every send/receive is framed with RFC 3947/3948's 4-byte non-ESP marker
	floatAnyPort   bool // may float to an ephemeral local port when 4500 is taken (a second IKE SA alongside a live one)
}

// NAT-T ports (RFC 3947): the server's is fixed at 4500 and ours is 4500 too
// unless that is taken. Variables only so tests can run a fake server on
// loopback next to the client.
var (
	natTServerPort = 4500
	natTLocalPort  = 4500
)

// nonESPMarker is RFC 3947 §3's 4 zero bytes prepended to every IKE (not
// ESP) message once negotiation has floated to UDP/4500 — required so the
// receiver can tell an IKE control message apart from a UDP-encapsulated
// ESP packet arriving on the same port.
var nonESPMarker = []byte{0, 0, 0, 0}

// Config is everything needed to run Phase 1 against one server.
type Config struct {
	// Reauth marks this Phase 1 as a second IKE SA built while an earlier one
	// is still carrying traffic (see the engine's re-authentication): it may
	// then float to an ephemeral local port instead of failing on a taken 4500.
	Reauth bool
	// LocalPorts overrides the local ports tried for the pre-NAT-T exchange
	// (default 500 then any free port). Port 500 needs root; an unprivileged
	// run passes {0}.
	LocalPorts []int
	ServerHost string
	ServerID   string // expected IDr; empty means accept any (matches entrypoint.sh's rightid=%any)
	PSK        string
	Proposals  []string // e.g. entrypoint.sh's ike= list, most-preferred first
	LocalIP    net.IP   // our outbound address, used as our ID_IPV4_ADDR (matches left=%defaultroute)
	// L2TPPort is our side's L2TP UDP port, the port in Quick Mode's IDci
	// (0 = L2TPPort's standard 1701). See Session.L2TPPort.
	L2TPPort uint16
}

// EstablishPhase1 runs IKEv1 Main Mode with PSK authentication end-to-end
// (MM1–MM6) against cfg.ServerHost:500, including RFC 3947 NAT-T detection
// and, if NAT is found, floating to :4500 for MM5/MM6 as required.
func EstablishPhase1(ctx context.Context, cfg Config) (*Session, error) {
	return establishPhase1To(ctx, cfg, 500)
}

// establishPhase1To is EstablishPhase1 against an arbitrary server port, so
// tests can run against a local responder.
func establishPhase1To(ctx context.Context, cfg Config, serverPort int) (*Session, error) {
	transforms := make([]Transform, 0, len(cfg.Proposals))
	for _, p := range cfg.Proposals {
		t, err := ParseProposal(p)
		if err != nil {
			return nil, fmt.Errorf("configured IKE proposal: %w", err)
		}
		transforms = append(transforms, t)
	}

	serverAddr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(cfg.ServerHost, strconv.Itoa(serverPort)))
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", cfg.ServerHost, err)
	}
	// Bind our own source port to 500 for the pre-NAT-T exchange: some IKE responders key their
	// NAT-D "my view of your address" computation, and some firewalls'
	// UDP/500 conntrack entries, on the client also using port 500 for the
	// pre-NAT-T exchange — matching what every real IKE client does.
	//
	// Bind to cfg.LocalIP specifically, not the wildcard address: NAT-T
	// replaces this UDP/500 socket with one bound to UDP/4500 after MM4.
	// L2TP's "port 1701" is a virtual header this client builds
	// inside the ESP payload in internal/engine/transport.go, never a real
	// socket, so there's nothing to bind there). A wildcard bind lets the
	// kernel repick the source address/interface for every sendto against
	// whatever the routing table says *at that moment* — exactly what
	// bit us once ApplyFullTunnel's split-default routes are in the
	// picture and something reshuffles the route table. Pinning to a
	// specific local address removes that ambiguity.
	var lastErr error
	ports := phase1LocalPorts
	if len(cfg.LocalPorts) > 0 {
		ports = cfg.LocalPorts
	}
	for i, port := range ports {
		last := i+1 == len(ports)
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: cfg.LocalIP, Port: port})
		if err != nil {
			lastErr = fmt.Errorf("bind local UDP/%d on %s (needs root, or another IKE client is already using it): %w", port, cfg.LocalIP, err)
			if !last && isAddrInUse(err) {
				vpnlog.Error(stage, "local UDP/500 is taken — retrying from another port", vpnlog.Fields{"err": err})
				continue
			}
			return nil, lastErr
		}
		sess := &Session{conn: conn, serverIP: serverAddr.IP, destAddr: serverAddr, LocalIP: cfg.LocalIP, floatAnyPort: cfg.Reauth, L2TPPort: cfg.L2TPPort}
		_ = conn.SetReadBuffer(8 * 1024 * 1024)
		_ = conn.SetWriteBuffer(8 * 1024 * 1024)
		if !last {
			// Leave time for the fallback inside the caller's connect timeout.
			sess.mm1Retransmits = firstPortMM1Retransmits
		}
		if _, err := rand.Read(sess.InitiatorSPI[:]); err != nil {
			conn.Close()
			return nil, fmt.Errorf("generate initiator SPI: %w", err)
		}
		err = sess.runMainMode(ctx, cfg, transforms)
		if err == nil {
			return sess, nil
		}
		conn.Close()
		lastErr = err
		if !last && errors.Is(err, errMM1Unanswered) {
			vpnlog.Error(stage, "no answer to MM1 from local UDP/500 — retrying from another port", nil)
			continue
		}
		return nil, err
	}
	return nil, lastErr
}

// phase1LocalPorts are the local ports tried, in order, for the pre-NAT-T
// exchange. 500 first: some responders and firewalls expect an IKE client
// to use it (see the bind comment above). Then any free port, because some
// networks drop or swallow IKE sourced from UDP/500 — e.g. a router's
// "IPsec passthrough" that admits a single client per server, or one still
// holding state for an earlier client — while answering other ports fine
// (confirmed live). A non-500 source port shows up as NAT in NAT-D, so the
// exchange then floats to UDP/4500 as usual.
var phase1LocalPorts = []int{500, 0}

// firstPortMM1Retransmits keeps the UDP/500 attempt short (2 sends, ~6s)
// so the fallback still fits a 30s connect timeout.
const firstPortMM1Retransmits = 1

// errMM1Unanswered marks a Main Mode that failed because MM1 got no reply at
// all — the only failure worth retrying from another local port; anything
// later means the server did answer.
var errMM1Unanswered = errors.New("MM1 unanswered")

func isAddrInUse(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }

// ErrNoResponse is wrapped by every exchange that exhausted its retransmits.
var ErrNoResponse = errors.New("no response")

// exchange sends msg to s.destAddr and waits for a reply from the server's
// IP on *any* source port (see the Session doc comment for why). It keeps
// reading (without re-sending) as long as replies keep arriving but don't
// look like the expected next message — an encrypted Informational/Notify
// interleaved with the real reply is normal IKE traffic, not a failure —
// only a full retransmit-budget of silence is IKE_TIMEOUT.
func (s *Session) exchange(ctx context.Context, msg []byte, want mmReply) ([]byte, error) {
	return s.exchangeN(ctx, msg, want, maxRetransmits)
}

func (s *Session) mm1RetransmitBudget() int {
	if s.mm1Retransmits > 0 {
		return s.mm1Retransmits
	}
	return maxRetransmits
}

// calcRetransmitInterval returns an adaptive backoff interval starting faster (1s, 2s, 3s)
// while respecting any shortened test intervals.
func calcRetransmitInterval(attempt int) time.Duration {
	if retransmitInterval < time.Second {
		return retransmitInterval
	}
	switch attempt {
	case 0:
		return 1 * time.Second
	case 1:
		return 2 * time.Second
	default:
		return retransmitInterval
	}
}

// exchangeN is exchange with an explicit retransmit budget.
func (s *Session) exchangeN(ctx context.Context, msg []byte, want mmReply, retransmits int) ([]byte, error) {
	wire := msg
	if s.floated {
		wire = append(append([]byte{}, nonESPMarker...), msg...)
	}

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = s.conn.SetReadDeadline(time.Now())
		case <-stop:
		}
	}()

	var lastErr error
	for attempt := 0; attempt <= retransmits; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt > 0 {
			vpnlog.Debug(stage, "retransmitting", vpnlog.Fields{"attempt": attempt})
		}
		if _, err := s.conn.WriteToUDP(wire, s.destAddr); err != nil {
			return nil, fmt.Errorf("send: %w", err)
		}
		interval := calcRetransmitInterval(attempt)
		deadline := time.Now().Add(interval)
		for {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			remaining := time.Until(deadline)
			if remaining <= 0 {
				lastErr = fmt.Errorf("no reply within %s", interval)
				break
			}
			s.conn.SetReadDeadline(deadline)
			buf := make([]byte, 65535)
			n, from, err := s.conn.ReadFromUDP(buf)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				lastErr = err
				break
			}
			if !from.IP.Equal(s.serverIP) {
				continue // stray packet from something else on this port
			}
			got := buf[:n]
			floatedMsg := from.Port == natTServerPort
			if floatedMsg {
				if n < 4 || !bytes.Equal(got[:4], nonESPMarker) {
					vpnlog.Debug(stage, "dropped :4500 packet missing non-ESP marker", vpnlog.Fields{"bytes": n})
					continue
				}
				got = got[4:]
			}
			if len(got) < headerLen {
				continue
			}
			h, err := ParseHeader(got)
			if err != nil {
				continue
			}
			if nt, ok := s.plainErrorNotify(h, got[headerLen:]); ok {
				return nil, fmt.Errorf("%w: Notify %d", ErrServerRefused, nt)
			}
			if h.ExchangeType == ExchangeInformational {
				vpnlog.Info(stage, "informational SPIs", vpnlog.Fields{
					"got_i": fmt.Sprintf("%x", h.InitiatorSPI), "got_r": fmt.Sprintf("%x", h.ResponderSPI),
					"our_i": fmt.Sprintf("%x", s.InitiatorSPI), "our_r": fmt.Sprintf("%x", s.ResponderSPI),
				})
				s.logInformational(h, got[headerLen:])
				continue // not the message this call is waiting for — keep listening
			}
			if !s.isReply(h, want) {
				// A duplicate of an earlier reply (the server answering one of
				// our retransmits), another IKE SA's traffic, or a forgery:
				// never the message this step waits for.
				vpnlog.Debug(stage, "ignored a message that is not the expected Main Mode reply", vpnlog.Fields{
					"exchange": h.ExchangeType, "next_payload": h.NextPayload, "flags": h.Flags,
				})
				continue
			}
			return got, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	return nil, fmt.Errorf("IKE_TIMEOUT: %w after %d attempts: %v", ErrNoResponse, retransmits+1, lastErr)
}

// SendESP sends one already-framed ESP packet (SPI|Seq|IV|ciphertext|ICV,
// see internal/ipsec) over this session's socket. ESP packets get no
// non-ESP marker — RFC 3948 §2.1: the receiver tells IKE and ESP traffic on
// the same port 4500 apart by checking whether the first 4 bytes are all
// zero (IKE) or not (ESP; a real SPI is never zero).
func (s *Session) SendESP(pkt []byte) error {
	_, err := s.conn.WriteToUDP(pkt, s.destAddr)
	return err
}

// RecvESP blocks for the next ESP packet from the server, transparently
// consuming (and logging) any interleaved IKE Informational message instead
// of returning it — mirrors exchangeQuickMode's tolerance for keepalives.
func (s *Session) RecvESP(ctx context.Context) ([]byte, error) {
	if s.dp != nil {
		return s.recvESPDataPhase(ctx)
	}
	for {
		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Now().Add(30 * time.Second)
		}
		if err := s.conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		buf := make([]byte, 65535)
		n, from, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return nil, err
		}
		if !from.IP.Equal(s.serverIP) {
			continue
		}
		got := buf[:n]
		if n >= 4 && bytes.Equal(got[:4], nonESPMarker) {
			// An IKE control message (Informational/DPD) arrived
			// interleaved with ESP traffic — handle and keep waiting.
			if n >= 4+headerLen {
				if h, err := ParseHeader(got[4:]); err == nil {
					s.logInformational(h, got[4+headerLen:])
				}
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			continue
		}
		return got, nil
	}
}

// sendRaw writes one already-framed message (marker added if floated) to
// the current destAddr, no retransmit/response handling — used for QM3,
// which per RFC 2409 §5.5 is a one-shot acknowledgement the initiator does
// not wait for a reply to.
func (s *Session) sendRaw(msg []byte) error {
	wire := msg
	if s.floated {
		wire = append(append([]byte{}, nonESPMarker...), msg...)
	}
	_, err := s.conn.WriteToUDP(wire, s.destAddr)
	return err
}

// exchangeQuickMode retransmits msg until a reply carrying the same
// Message-ID and ExchangeType==QuickMode arrives, the same
// keep-listening-past-Informational behavior as exchange().
func (s *Session) exchangeQuickMode(msg []byte, msgID uint32) ([]byte, error) {
	wire := msg
	if s.floated {
		wire = append(append([]byte{}, nonESPMarker...), msg...)
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetransmits; attempt++ {
		if attempt > 0 {
			vpnlog.Debug(stage, "QM retransmitting", vpnlog.Fields{"attempt": attempt})
		}
		if _, err := s.conn.WriteToUDP(wire, s.destAddr); err != nil {
			return nil, fmt.Errorf("send: %w", err)
		}
		deadline := time.Now().Add(retransmitInterval)
		for {
			if time.Until(deadline) <= 0 {
				lastErr = fmt.Errorf("no reply within %s", retransmitInterval)
				break
			}
			s.conn.SetReadDeadline(deadline)
			buf := make([]byte, 65535)
			n, from, err := s.conn.ReadFromUDP(buf)
			if err != nil {
				lastErr = err
				break
			}
			if !from.IP.Equal(s.serverIP) {
				continue
			}
			got := buf[:n]
			if from.Port == natTServerPort {
				if n < 4 || !bytes.Equal(got[:4], nonESPMarker) {
					continue
				}
				got = got[4:]
			}
			if len(got) < headerLen {
				continue
			}
			h, err := ParseHeader(got)
			if err != nil {
				continue
			}
			if h.ExchangeType == ExchangeInformational {
				if nt := s.logInformational(h, got[headerLen:]); nt != 0 && nt < notifyErrorLimit {
					return nil, fmt.Errorf("%w: Notify %d", ErrQuickModeRejected, nt)
				}
				continue
			}
			if h.MessageID != msgID {
				continue // reply to a different exchange — keep waiting
			}
			return got, nil
		}
	}
	return nil, fmt.Errorf("IKE_TIMEOUT: Quick Mode: no response after %d attempts: %v", maxRetransmits+1, lastErr)
}

// logInformational best-effort decrypts a Notify/Informational exchange
// using whatever Phase 1 keys have been derived so far, purely for
// diagnostics — the server sends these for real protocol reasons (rejecting
// a message, DPD, SA deletion) and silently dropping them without at least
// logging the notify type makes failures much harder to diagnose.
//
// It returns the first Notify type it found (0 for none), so a caller can
// stop waiting on an exchange the server has refused.
func (s *Session) logInformational(h Header, encBody []byte) (notifyType uint16) {
	if s.Keys == nil || len(encBody) == 0 {
		vpnlog.Info(stage, "received Informational exchange (no keys yet to decrypt)", nil)
		return 0
	}
	bs := blockSize(s.Transform)
	if len(encBody)%bs != 0 || s.phase1IV == nil {
		vpnlog.Info(stage, "received Informational exchange (undecryptable)", nil)
		return 0
	}
	// RFC 2409 §5.5: a new exchange (Informational or Quick Mode) does not
	// reuse the last Phase 1 ciphertext block as its IV directly — it seeds
	// a fresh one from hash(last Phase 1 IV | this message's Message-ID).
	iv, err := informationalIV(s.Transform.Hash, s.phase1IV, h.MessageID, bs)
	if err != nil {
		vpnlog.Info(stage, "received Informational exchange (IV derivation failed)", vpnlog.Fields{"err": err})
		return 0
	}
	plain, err := cbcDecrypt(s.Transform, s.Keys.EncKey, iv, encBody)
	if err != nil {
		vpnlog.Info(stage, "received Informational exchange (decrypt failed)", vpnlog.Fields{"err": err})
		return 0
	}
	payloads, err := SplitPayloads(h.NextPayload, plain)
	if err != nil {
		vpnlog.Info(stage, "received Informational exchange (undecodable after decrypt)", vpnlog.Fields{
			"next_payload": h.NextPayload, "plain_len": len(plain), "parse_err": err,
		})
		return 0
	}
	for _, p := range payloads {
		if p.Type == PayloadNotify && len(p.Body) >= 8 {
			nt := uint16(p.Body[6])<<8 | uint16(p.Body[7])
			vpnlog.Info(stage, "server sent Notify", vpnlog.Fields{"notify_type": nt})
			if notifyType == 0 {
				notifyType = nt
			}
		}
	}
	return notifyType
}

// buildMM1 is Main Mode's first message, HDR, SA, VID(DPD), VID(NAT-T RFC3947), VID(NAT-T draft-02) — shared by
// the real exchange and ProbeResponder (probe.go) so a probe is exactly what connect
// would send.
func buildMM1(initiatorSPI [8]byte, sa []byte) []byte {
	dpd := marshalPayload(PayloadVendorID, dpdVendorID)
	vidRFC := marshalPayload(PayloadVendorID, RFC3947VendorID())
	vidDraft := marshalPayload(PayloadNone, Draft02NVendorID())
	body := append(append(append(marshalPayload(PayloadVendorID, sa), dpd...), vidRFC...), vidDraft...)
	hdr := Header{InitiatorSPI: initiatorSPI, NextPayload: PayloadSA, Version: 0x10, ExchangeType: ExchangeIdentityProt}
	hdr.Length = uint32(headerLen + len(body))
	return append(hdr.Marshal(), body...)
}

func (s *Session) runMainMode(ctx context.Context, cfg Config, transforms []Transform) error {
	start := time.Now()

	sa := MarshalSA(transforms) // also SAi_b for HASH_I/HASH_R below
	mm1 := buildMM1(s.InitiatorSPI, sa)

	vpnlog.Info(stage, "MM1 sent (SA proposal)", vpnlog.Fields{"proposals": cfg.Proposals})
	resp, err := s.exchangeN(ctx, mm1, mmReply{firstPayloads: []uint8{PayloadSA, PayloadNotify}}, s.mm1RetransmitBudget())
	if err != nil {
		if errors.Is(err, ErrNoResponse) {
			return fmt.Errorf("MM1/MM2: %w: %w", errMM1Unanswered, err)
		}
		if errors.Is(err, ErrServerRefused) {
			return fmt.Errorf("IKE_PROPOSAL_MISMATCH: MM1/MM2: %w (NO_PROPOSAL_CHOSEN is 14)", err)
		}
		return fmt.Errorf("MM1/MM2: %w", err)
	}
	hdr2, err := ParseHeader(resp)
	if err != nil {
		return err
	}
	s.ResponderSPI = hdr2.ResponderSPI
	payloads2, err := SplitPayloads(hdr2.NextPayload, resp[headerLen:])
	if err != nil {
		return fmt.Errorf("parse MM2: %w", err)
	}
	var chosenSABody []byte
	peerSupportsNATT := false
	for _, p := range payloads2 {
		switch p.Type {
		case PayloadSA:
			chosenSABody = p.Body
		case PayloadVendorID:
			if IsNATTVendorID(p.Body) {
				peerSupportsNATT = true
			}
		case PayloadNotify:
			return fmt.Errorf("IKE_PROPOSAL_MISMATCH: server sent Notify instead of SA in MM2 (likely NO_PROPOSAL_CHOSEN)")
		}
	}

	if chosenSABody == nil {
		return fmt.Errorf("IKE_PROPOSAL_MISMATCH: MM2 did not contain an SA payload")
	}
	chosen, err := ParseChosenTransform(chosenSABody)
	if err != nil {
		return fmt.Errorf("IKE_PROPOSAL_MISMATCH: %w", err)
	}
	// The responder may only pick one of our transforms. Main Mode's HASH_R
	// covers the SA we offered (SAi_b), not this choice, so nothing later
	// would catch a substituted one.
	if !offeredIKE(chosen, transforms) {
		return fmt.Errorf("IKE_PROPOSAL_MISMATCH: server chose a transform we never offered (encryption %d, %d-bit key, hash %d, group %d)", chosen.Encryption, cipherKeyLen(chosen)*8, chosen.Hash, chosen.Group)
	}
	s.Transform = chosen
	vpnlog.Info(stage, "MM2 received (server agreed on IKE proposal)", vpnlog.Fields{
		"transform": chosen.String(), "nat_t_detected": peerSupportsNATT,
	})

	// --- MM3: HDR, KE, Nonce[, NAT-D, NAT-D] ---
	group, ok := Groups[chosen.Group]
	if !ok {
		return fmt.Errorf("server chose unsupported DH group %d", chosen.Group)
	}
	kp, err := GenerateKeyPair(group)
	if err != nil {
		return err
	}
	ni := make([]byte, 32)
	if _, err := rand.Read(ni); err != nil {
		return err
	}

	kePayload := marshalPayload(PayloadNonce, kp.PublicBytes())
	var nonceNext uint8 = PayloadNone
	var natdBody []byte
	if peerSupportsNATT {
		nonceNext = payloadNATD
	}
	noncePayload := marshalPayload(nonceNext, ni)
	body3 := append(kePayload, noncePayload...)

	if peerSupportsNATT {
		localPort := uint16(500)
		natdLocal, err := computeNATD(chosen.Hash, s.InitiatorSPI, s.ResponderSPI, cfg.LocalIP, localPort)
		if err != nil {
			return err
		}
		natdRemote, err := computeNATD(chosen.Hash, s.InitiatorSPI, s.ResponderSPI, s.serverIP, 500)
		if err != nil {
			return err
		}
		natdLocalPayload := marshalPayload(payloadNATD, natdLocal)
		natdRemotePayload := marshalPayload(PayloadNone, natdRemote)
		natdBody = append(natdLocalPayload, natdRemotePayload...)
	}
	body3 = append(body3, natdBody...)

	hdr3 := Header{InitiatorSPI: s.InitiatorSPI, ResponderSPI: s.ResponderSPI, NextPayload: PayloadKE, Version: 0x10, ExchangeType: ExchangeIdentityProt}
	hdr3.Length = uint32(headerLen + len(body3))
	mm3 := append(hdr3.Marshal(), body3...)

	vpnlog.Info(stage, "MM3 sent (KE, nonce)", vpnlog.Fields{"group": chosen.Group})
	resp4, err := s.exchange(ctx, mm3, mmReply{firstPayloads: []uint8{PayloadKE}})
	if err != nil {
		return fmt.Errorf("MM3/MM4: %w", err)
	}
	hdr4, err := ParseHeader(resp4)
	if err != nil {
		return err
	}
	payloads4, err := SplitPayloads(hdr4.NextPayload, resp4[headerLen:])
	if err != nil {
		return fmt.Errorf("parse MM4: %w", err)
	}
	var peerKE, peerNonce []byte
	var peerNATDs [][]byte
	for _, p := range payloads4 {
		switch p.Type {
		case PayloadKE:
			peerKE = p.Body
		case PayloadNonce:
			peerNonce = p.Body
		case payloadNATD:
			peerNATDs = append(peerNATDs, p.Body)
		}
	}
	if peerKE == nil || peerNonce == nil {
		return fmt.Errorf("IKE_AUTH_FAILED: MM4 missing KE or Nonce payload")
	}

	if peerSupportsNATT && len(peerNATDs) >= 2 {
		expectMine, _ := computeNATD(chosen.Hash, s.InitiatorSPI, s.ResponderSPI, cfg.LocalIP, 500)
		expectServer, _ := computeNATD(chosen.Hash, s.InitiatorSPI, s.ResponderSPI, s.serverIP, 500)
		matchMine := false
		matchServer := false
		for _, natd := range peerNATDs {
			if bytes.Equal(expectMine, natd) {
				matchMine = true
			}
			if bytes.Equal(expectServer, natd) {
				matchServer = true
			}
		}
		s.NATDetected = !matchMine || !matchServer
	}
	vpnlog.Info(stage, "MM4 received", vpnlog.Fields{"nat_detected": s.NATDetected})

	gxy, err := kp.SharedSecret(peerKE)
	if err != nil {
		return fmt.Errorf("MM4: %w", err)
	}
	keys, err := DerivePhase1Keys(chosen, []byte(cfg.PSK), ni, peerNonce, gxy, s.InitiatorSPI, s.ResponderSPI)
	if err != nil {
		return fmt.Errorf("derive Phase 1 keys: %w", err)
	}
	s.Keys = keys

	if s.NATDetected {
		// RFC 3947 §4 requires both ports to change to 4500 before MM5.
		// Keeping the local socket on :500 works through permissive NATs,
		// but IPsec-aware NATs can special-case that port and drop NAT-T ESP.
		if err := s.floatToNATT(cfg.LocalIP); err != nil {
			return err
		}
	}

	// IV0 = hash(g^xi | g^xr), truncated to block size — RFC 2409 §5.
	ivSeed, err := digest(chosen.Hash, append(append([]byte{}, kp.PublicBytes()...), peerKE...))
	if err != nil {
		return err
	}
	bs := blockSize(chosen)
	s.lastIV = ivSeed[:bs]

	// --- MM5*: HDR*, IDii, HASH_I (encrypted) ---
	idBody := MarshalIPv4ID(cfg.LocalIP)
	saBodyForHash := sa // SAi_b is the Phase 1 SA payload body sent in MM1 (without generic header)

	hashI, err := keys.ComputeHashI(kp.PublicBytes(), peerKE, s.InitiatorSPI, s.ResponderSPI, saBodyForHash, idBody)
	if err != nil {
		return err
	}

	idPayload := marshalPayload(PayloadHash, idBody)
	hashPayload := marshalPayload(PayloadNone, hashI)
	plain5 := append(idPayload, hashPayload...)
	padded5 := padToBlock(plain5, bs)
	cipher5, err := cbcEncrypt(chosen, keys.EncKey, s.lastIV, padded5)
	if err != nil {
		return err
	}
	s.lastIV = cipher5[len(cipher5)-bs:]

	hdr5 := Header{InitiatorSPI: s.InitiatorSPI, ResponderSPI: s.ResponderSPI, NextPayload: PayloadID, Version: 0x10, ExchangeType: ExchangeIdentityProt, Flags: FlagEncryption}
	hdr5.Length = uint32(headerLen + len(cipher5))
	mm5 := append(hdr5.Marshal(), cipher5...)

	vpnlog.Info(stage, "MM5 sent (encrypted identity + auth)", nil)
	resp6, err := s.exchange(ctx, mm5, mmReply{encrypted: true})
	if err != nil {
		return fmt.Errorf("MM5/MM6: %w", err)
	}
	hdr6, err := ParseHeader(resp6)
	if err != nil {
		return err
	}
	encBody6 := resp6[headerLen:]
	ivIn := s.lastIV
	plain6, err := cbcDecrypt(chosen, keys.EncKey, ivIn, encBody6)
	if err != nil {
		return fmt.Errorf("decrypt MM6: %w", err)
	}
	if len(encBody6) >= bs {
		s.lastIV = encBody6[len(encBody6)-bs:]
	}
	payloads6, err := SplitPayloads(hdr6.NextPayload, plain6)
	if err != nil {
		return fmt.Errorf("IKE_AUTH_FAILED: parse MM6 (likely wrong PSK): %w", err)
	}
	var peerIDBody, peerHash []byte
	for _, p := range payloads6 {
		switch p.Type {
		case PayloadID:
			peerIDBody = p.Body
		case PayloadHash:
			peerHash = p.Body
		}
	}
	if peerIDBody == nil || peerHash == nil {
		return fmt.Errorf("IKE_AUTH_FAILED: MM6 missing ID or HASH payload (wrong PSK, or server rejected our identity)")
	}
	expectHashR, err := keys.ComputeHashR(peerKE, kp.PublicBytes(), s.ResponderSPI, s.InitiatorSPI, saBodyForHash, peerIDBody)
	if err != nil {
		return err
	}
	if !hmac.Equal(expectHashR, peerHash) {
		return fmt.Errorf("IKE_AUTH_FAILED: HASH_R mismatch (PSK likely incorrect)")
	}
	if err := checkServerID(cfg.ServerID, peerIDBody); err != nil {
		return err
	}

	s.nextMsgID = 0
	// Every later exchange on this IKE SA (Quick Mode, Informational)
	// derives its first IV from this block, RFC 2409 §5.5 — not from
	// whatever the most recent exchange left behind.
	s.phase1IV = append([]byte{}, s.lastIV...)
	s.EstablishedAt = time.Now()
	s.Lifetime = time.Duration(chosen.LifeSecs) * time.Second
	vpnlog.Timed(stage, "Phase 1 ESTABLISHED", start)
	vpnlog.Info(stage, "IKE SA lifetime", vpnlog.Fields{"seconds": chosen.LifeSecs})
	return nil
}

// floatToNATT replaces the pre-negotiation UDP/500 socket with UDP/4500.
// Bind first so failure leaves the established Phase 1 socket intact.
func (s *Session) floatToNATT(localIP net.IP) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: localIP, Port: natTLocalPort})
	if err != nil && s.floatAnyPort && isAddrInUse(err) {
		// A second IKE SA built while the first is still live (re-authentication)
		// cannot share local UDP/4500 with it. NAT-T only fixes the *server's*
		// port; any local source port works, the server just answers to whatever
		// it saw.
		conn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: localIP, Port: 0})
	}
	if err != nil {
		return fmt.Errorf("bind local UDP/%d for NAT-T on %s: %w", natTLocalPort, localIP, err)
	}
	_ = conn.SetReadBuffer(8 * 1024 * 1024)
	_ = conn.SetWriteBuffer(8 * 1024 * 1024)
	oldConn := s.conn
	s.conn = conn
	s.destAddr = &net.UDPAddr{IP: s.serverIP, Port: natTServerPort}
	s.floated = true
	if err := oldConn.Close(); err != nil {
		conn.Close()
		s.conn = oldConn
		s.destAddr = &net.UDPAddr{IP: s.serverIP, Port: 500}
		s.floated = false
		return fmt.Errorf("close pre-NAT-T UDP/500 socket: %w", err)
	}
	vpnlog.Info(stage, "floated to UDP/4500 for NAT-T", vpnlog.Fields{"local_port": conn.LocalAddr().(*net.UDPAddr).Port})
	return nil
}

// informationalIV computes the IV a new exchange (Informational or Quick
// Mode) uses for its first encrypted message, RFC 2409 §5.5:
// IV = hash(last-Phase-1-IV | Message-ID), truncated to the cipher's block
// size. The Message-ID is the 4-byte big-endian value from that exchange's
// own ISAKMP header.
func informationalIV(hashAlg int, lastPhase1IV []byte, messageID uint32, blockLen int) ([]byte, error) {
	msgIDBytes := []byte{byte(messageID >> 24), byte(messageID >> 16), byte(messageID >> 8), byte(messageID)}
	seed, err := digest(hashAlg, append(append([]byte{}, lastPhase1IV...), msgIDBytes...))
	if err != nil {
		return nil, err
	}
	return seed[:blockLen], nil
}

// offeredIKE reports whether a Phase 1 choice matches one of our offers.
func offeredIKE(chosen Transform, offered []Transform) bool {
	for _, o := range offered {
		if o.Encryption == chosen.Encryption && cipherKeyLen(o) == cipherKeyLen(chosen) &&
			o.Hash == chosen.Hash && o.Group == chosen.Group && o.AuthMethod == chosen.AuthMethod {
			return true
		}
	}
	return false
}

// checkServerID enforces the profile's server_id against the responder's
// (HASH_R-authenticated) ID payload. Empty want accepts any identity. Every
// ID type is compared — an identity type this client doesn't recognize, or
// an unparsable payload, is a mismatch rather than a silent pass.
func checkServerID(want string, peerIDBody []byte) error {
	if want == "" {
		return nil
	}
	pid, err := ParseID(peerIDBody)
	if err != nil {
		return fmt.Errorf("IKE_AUTH_FAILED: parse server ID payload: %w", err)
	}
	if got := pid.String(); got != want {
		return fmt.Errorf("IKE_AUTH_FAILED: server identified itself as %s, expected %s (set server_id to match, or leave empty to accept any)", got, want)
	}
	return nil
}

// payloadNATD is RFC 3947 §5's NAT-D payload type, registered as ISAKMP
// payload type 20. It postdates the original RFC 2408 payload registry, so
// it is not one of the PayloadXxx constants in isakmp.go.
const payloadNATD = 20

// mmReply describes the Main Mode reply one step waits for (RFC 2409 §5):
// MM2 opens with SA (or a Notify refusing it), MM4 with KE, and MM6 is the
// first encrypted one.
type mmReply struct {
	firstPayloads []uint8 // for an unencrypted reply: the payload types it may open with
	encrypted     bool
}

// isReply reports whether h is the reply want describes, on this IKE SA. The
// shape check is what keeps a late duplicate MM2 from being read as MM4, or
// a duplicate MM4 as MM6 (and blamed on a wrong PSK).
func (s *Session) isReply(h Header, want mmReply) bool {
	if h.ExchangeType != ExchangeIdentityProt || h.InitiatorSPI != s.InitiatorSPI {
		return false
	}
	if s.ResponderSPI != ([8]byte{}) && h.ResponderSPI != s.ResponderSPI {
		return false
	}
	if (h.Flags&FlagEncryption != 0) != want.encrypted {
		return false
	}
	if want.encrypted {
		return true
	}
	for _, t := range want.firstPayloads {
		if h.NextPayload == t {
			return true
		}
	}
	return false
}

// ErrServerRefused is wrapped when the server answers a Main Mode step with
// an error Notify instead of the next message.
var ErrServerRefused = errors.New("server refused")

// plainErrorNotify returns the type of an unencrypted error Notify (RFC 2408
// §3.14.1: types below 16384) the server sent on this IKE SA — how a
// responder refuses a Main Mode, e.g. NO-PROPOSAL-CHOSEN in place of MM2.
// It carries no authentication: only a message bearing our own initiator
// cookie, which an off-path sender cannot know, is taken into account.
func (s *Session) plainErrorNotify(h Header, body []byte) (uint16, bool) {
	if h.Flags&FlagEncryption != 0 || h.InitiatorSPI != s.InitiatorSPI {
		return 0, false
	}
	if h.ExchangeType != ExchangeInformational && h.ExchangeType != ExchangeIdentityProt {
		return 0, false
	}
	payloads, err := SplitPayloads(h.NextPayload, body)
	if err != nil {
		return 0, false
	}
	for _, p := range payloads {
		if p.Type == PayloadNotify && len(p.Body) >= 8 {
			if nt := uint16(p.Body[6])<<8 | uint16(p.Body[7]); nt != 0 && nt < 16384 {
				return nt, true
			}
		}
	}
	return 0, false
}
