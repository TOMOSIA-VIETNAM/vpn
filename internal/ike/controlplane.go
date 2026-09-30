package ike

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"vpn/internal/bufpool"
	"vpn/internal/vpnlog"
)

// Events are the control-plane notifications StartDataPhase delivers. Both
// are only ever called for messages whose HASH(1) verified under this IKE
// SA — an unauthenticated Delete could otherwise tear the tunnel down.
type Events struct {
	// DeleteESP reports the peer deleted the ESP SAs with these SPIs.
	DeleteESP func(spis []uint32)
	// DeleteIKE reports the peer deleted this IKE SA: no further Quick Mode
	// (and so no rekey) is possible on it.
	DeleteIKE func()

	// ESPProposals and NewChildSA together enable answering a rekey the peer
	// starts itself (see responder_qm.go): ESPProposals are the transforms we
	// accept, NewChildSA receives the finished SA pair, which should become the
	// one outbound traffic uses. Leave them unset to ignore such rekeys.
	ESPProposals []string
	NewChildSA   func(*QuickModeResult)
}

// dataPlane is the state StartDataPhase adds to a Session.
type dataPlane struct {
	espIn  chan []byte
	done   chan struct{} // closed when the reader exits
	errMu  sync.Mutex
	err    error // why the reader exited
	events Events

	pendingMu sync.Mutex
	pending   map[uint32]chan []byte // Quick Mode replies we are waiting for, by message ID

	respMu sync.Mutex
	resp   map[uint32]*respQM // server-initiated Quick Modes we have answered, by message ID
}

// maxReadErrors is how many consecutive socket read errors (spaced
// readErrorBackoff apart) the reader tolerates before giving up: a network
// change or wake from sleep can fail reads for a moment, and one such blip
// must not end a session that the liveness watchdog would have judged.
const (
	maxReadErrors    = 50
	readErrorBackoff = 200 * time.Millisecond
)

// natKeepalive is RFC 3948 §2.3's NAT keepalive: one 0xFF byte in a UDP
// datagram, silently discarded by the receiver.
var natKeepalive = []byte{0xFF}

// Age is how long ago this IKE SA was established.
func (s *Session) Age() time.Duration { return time.Since(s.EstablishedAt) }

// IKELifetime is the lifetime the responder chose for this IKE SA (0 if it
// announced none).
func (s *Session) IKELifetime() time.Duration { return s.Lifetime }

// SendNATKeepalive refreshes the NAT mapping for the floated UDP/4500 flow
// (RFC 3948 §2.3). Home/office NATs commonly forget an idle UDP mapping
// within 30–120s, after which the server's packets are dropped at the
// router while everything on this side still looks connected. A no-op when
// no NAT was detected (not floated).
func (s *Session) SendNATKeepalive() error {
	if !s.floated {
		return nil
	}
	_, err := s.conn.WriteToUDP(natKeepalive, s.destAddr)
	return err
}

// StartDataPhase hands the socket to a single reader goroutine for the rest
// of the session. Until now each exchange read the socket itself, which
// only works while nothing else is: once ESP traffic flows, a rekey's
// Quick Mode reply and an ESP packet can arrive in either order on the same
// port, and whichever goroutine happened to be reading would swallow the
// other's datagram. The reader routes ESP to RecvESP, Quick Mode replies to
// the exchange waiting on that message ID, and Informational messages to
// handleInformational. Call once, right after the first Quick Mode.
func (s *Session) StartDataPhase(ctx context.Context, events Events) {
	dp := &dataPlane{
		espIn:   make(chan []byte, espQueueLen),
		done:    make(chan struct{}),
		events:  events,
		pending: map[uint32]chan []byte{},
		resp:    map[uint32]*respQM{},
	}
	s.dp = dp
	go s.readLoop(ctx, dp)
}

// espQueueLen bounds ESP packets buffered between the socket reader and the
// decrypt loop. It used to be 32768 (~50 MB of pinned packets in a burst, and
// seconds of queueing delay); the kernel's 8 MB socket buffer already absorbs
// bursts, and a full queue just makes the reader wait.
const espQueueLen = 1024

func (s *Session) readLoop(ctx context.Context, dp *dataPlane) {
	defer close(dp.done)
	buf := make([]byte, 65535)
	readErrors := 0
	// Block in the read instead of waking every second to check ctx: an idle
	// tunnel then costs no CPU wakeups at all. Cancelling ctx expires the
	// read deadline, which is what unblocks the read below. Clear any
	// deadline the negotiation phases left on the socket first.
	_ = s.conn.SetReadDeadline(time.Time{})
	stop := context.AfterFunc(ctx, func() { _ = s.conn.SetReadDeadline(time.Now()) })
	defer stop()
	for {
		if ctx.Err() != nil {
			dp.setErr(ctx.Err())
			return
		}
		n, from, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				readErrors = 0
				continue // only ctx cancellation expires the deadline; the check above ends the loop
			}
			readErrors++
			if errors.Is(err, net.ErrClosed) || readErrors > maxReadErrors {
				dp.setErr(err)
				return
			}
			vpnlog.Error(stage, "IKE/ESP socket read failed — retrying", vpnlog.Fields{"err": err, "consecutive": readErrors})
			select {
			case <-time.After(readErrorBackoff):
			case <-ctx.Done():
			}
			continue
		}
		readErrors = 0
		if !from.IP.Equal(s.serverIP) {
			continue
		}
		// Pooled: the consumer (engine espTransport.process) returns ESP packets
		// once decrypted. IKE messages and dropped packets are simply left to the GC.
		pkt := bufpool.Get(n)
		copy(pkt, buf[:n])
		if ike, ok := s.ikeMessage(pkt); ok {
			s.dispatchIKE(ike)
			continue
		}
		select {
		case dp.espIn <- pkt:
		case <-ctx.Done():
			dp.setErr(ctx.Err())
			return
		}
	}
}

// ikeMessage tells an IKE message apart from ESP on the shared socket and
// returns it without framing. Floated (RFC 3948 §2.1): IKE carries the
// 4-byte zero non-ESP marker, ESP never does (a real SPI is never zero).
// Not floated: an IKE message starts with this SA's initiator cookie.
func (s *Session) ikeMessage(pkt []byte) ([]byte, bool) {
	if len(pkt) >= 4 && bytes.Equal(pkt[:4], nonESPMarker) {
		return pkt[4:], true
	}
	if !s.floated && len(pkt) >= headerLen && bytes.Equal(pkt[:8], s.InitiatorSPI[:]) {
		return pkt, true
	}
	return nil, false
}

func (s *Session) dispatchIKE(msg []byte) {
	if len(msg) < headerLen {
		return
	}
	h, err := ParseHeader(msg)
	if err != nil {
		return
	}
	swapped := h.InitiatorSPI == s.ResponderSPI && h.ResponderSPI == s.InitiatorSPI
	if h.InitiatorSPI != s.InitiatorSPI && !swapped {
		// Not this SA: an earlier attempt of ours the server still holds
		// (it keeps probing it with DPD/Delete until its timeouts fire)
		// or a Main Mode it started itself. Either way there is nothing
		// to act on — the messages are under keys we no longer have.
		vpnlog.Info(stage, "ignored IKE message for a different IKE SA (stale earlier session on the server?)", vpnlog.Fields{
			"exchange": h.ExchangeType, "msg_id": h.MessageID,
			"initiator_spi": fmt.Sprintf("%x", h.InitiatorSPI), "responder_spi": fmt.Sprintf("%x", h.ResponderSPI),
		})
		return
	}
	switch h.ExchangeType {
	case ExchangeQuickMode:
		s.dp.pendingMu.Lock()
		ch := s.dp.pending[h.MessageID]
		s.dp.pendingMu.Unlock()
		if ch == nil {
			s.handleQuickMode(h, msg[headerLen:])
			return
		}
		select {
		case ch <- msg:
		default: // a duplicate while the first is still being processed
		}
	case ExchangeInformational:
		s.handleInformational(h, msg[headerLen:])
	default:
		vpnlog.Info(stage, "ignored IKE exchange during data phase", vpnlog.Fields{"exchange": h.ExchangeType})
	}
}

// handleInformational authenticates and acts on an Informational exchange
// (RFC 2409 §5.7): HASH(1) = prf(SKEYID_a, M-ID | N/D payloads).
func (s *Session) handleInformational(h Header, encBody []byte) {
	payloads, plain, err := s.decryptExchange(h, encBody)
	if err != nil {
		vpnlog.Error(stage, "undecodable Informational exchange", vpnlog.Fields{"err": err})
		return
	}
	if err := verifyHash1(s.Transform.Hash, s.Keys.SKEYIDa, h.MessageID, h.NextPayload, payloads, plain); err != nil {
		vpnlog.Error(stage, "Informational exchange failed authentication — ignored", vpnlog.Fields{"err": err})
		return
	}
	for _, p := range payloads[1:] {
		switch p.Type {
		case PayloadNotify:
			if len(p.Body) < 8 {
				continue
			}
			nt := binary.BigEndian.Uint16(p.Body[6:8])
			if nt == notifyRUThere {
				s.replyDPD(p.Body)
				continue
			}
			vpnlog.Info(stage, "server sent Notify", vpnlog.Fields{"notify_type": nt})
		case PayloadDelete:
			proto, spis, err := parseDelete(p.Body)
			if err != nil {
				vpnlog.Error(stage, "malformed Delete payload", vpnlog.Fields{"err": err})
				continue
			}
			switch proto {
			case protoISAKMP:
				vpnlog.Error(stage, "server deleted the IKE SA — no further rekey possible on it", nil)
				if s.dp.events.DeleteIKE != nil {
					s.dp.events.DeleteIKE()
				}
			case protoIPsecESP:
				vpnlog.Info(stage, "server deleted ESP SAs", vpnlog.Fields{"spis": fmt.Sprintf("%08x", spis)})
				if s.dp.events.DeleteESP != nil {
					s.dp.events.DeleteESP(spis)
				}
			}
		}
	}
}

// decryptExchange decrypts the first message of a peer-initiated exchange,
// whose IV derives from the Phase 1 IV and its message ID (RFC 2409 §5.5).
func (s *Session) decryptExchange(h Header, encBody []byte) ([]RawPayload, []byte, error) {
	bs := blockSize(s.Transform)
	if s.Keys == nil || s.phase1IV == nil || len(encBody) == 0 || len(encBody)%bs != 0 {
		return nil, nil, fmt.Errorf("not decryptable under this IKE SA")
	}
	iv, err := informationalIV(s.Transform.Hash, s.phase1IV, h.MessageID, bs)
	if err != nil {
		return nil, nil, err
	}
	plain, err := cbcDecrypt(s.Transform, s.Keys.EncKey, iv, encBody)
	if err != nil {
		return nil, nil, err
	}
	payloads, err := SplitPayloads(h.NextPayload, plain)
	if err != nil {
		return nil, nil, err
	}
	return payloads, plain, nil
}

const protoISAKMP = 1

// parseDelete decodes a Delete payload body (RFC 2408 §3.15): DOI(4),
// Protocol-Id(1), SPI Size(1), # of SPIs(2), SPIs. ISAKMP SA deletes carry
// 16-byte cookie pairs, so only ESP's 4-byte SPIs are returned as values.
func parseDelete(body []byte) (proto uint8, spis []uint32, err error) {
	if len(body) < 8 {
		return 0, nil, fmt.Errorf("Delete payload too short")
	}
	proto, spiSize, n := body[4], int(body[5]), int(binary.BigEndian.Uint16(body[6:8]))
	if len(body) < 8+spiSize*n {
		return 0, nil, fmt.Errorf("Delete payload lists %d SPIs of %d bytes but is %d bytes long", n, spiSize, len(body))
	}
	if spiSize == 4 {
		for i := 0; i < n; i++ {
			spis = append(spis, binary.BigEndian.Uint32(body[8+4*i:]))
		}
	}
	return proto, spis, nil
}

// verifyHash1 checks a peer-initiated message's leading HASH payload,
// prf(SKEYID_a, M-ID | rest) (RFC 2409 §5.5/§5.7).
func verifyHash1(hashAlg int, skeyidA []byte, msgID uint32, firstType uint8, payloads []RawPayload, plain []byte) error {
	return verifyLeadingHash(hashAlg, skeyidA, beUint32(msgID), firstType, payloads, plain)
}

// verifyLeadingHash checks that the message's first payload is
// prf(SKEYID_a, prefix | every payload after it), headers included and
// encryption padding excluded — the shape shared by HASH(1) and HASH(2).
func verifyLeadingHash(hashAlg int, skeyidA, prefix []byte, firstType uint8, payloads []RawPayload, plain []byte) error {
	if firstType != PayloadHash || len(payloads) == 0 {
		return fmt.Errorf("message does not start with a HASH payload")
	}
	hashLen := 4 + len(payloads[0].Body)
	total := 0
	for _, p := range payloads {
		total += 4 + len(p.Body)
	}
	want, err := prf(hashAlg, skeyidA, append(append([]byte{}, prefix...), plain[hashLen:total]...))
	if err != nil {
		return err
	}
	if !hmac.Equal(want, payloads[0].Body) {
		return fmt.Errorf("HASH mismatch — message not authenticated by the IKE SA")
	}
	return nil
}

// controlRoundTrip is Quick Mode's roundTrip during the data phase: send
// (with retransmits) and wait for the reader to route the reply here.
func (s *Session) controlRoundTrip(msg []byte, msgID uint32) ([]byte, error) {
	if s.dp == nil {
		return nil, fmt.Errorf("control plane not started")
	}
	ch := make(chan []byte, 1)
	s.dp.pendingMu.Lock()
	s.dp.pending[msgID] = ch
	s.dp.pendingMu.Unlock()
	defer func() {
		s.dp.pendingMu.Lock()
		delete(s.dp.pending, msgID)
		s.dp.pendingMu.Unlock()
	}()
	for attempt := 0; attempt <= maxRetransmits; attempt++ {
		if err := s.sendRaw(msg); err != nil {
			return nil, fmt.Errorf("send: %w", err)
		}
		select {
		case resp := <-ch:
			return resp, nil
		case <-s.dp.done:
			return nil, fmt.Errorf("control plane stopped: %w", s.dp.getErr())
		case <-time.After(retransmitInterval):
		}
	}
	return nil, fmt.Errorf("IKE_TIMEOUT: Quick Mode rekey: no response after %d attempts", maxRetransmits+1)
}

// RekeyQuickMode negotiates a fresh ESP SA pair on this IKE SA while the
// tunnel keeps running (RFC 2409 §5.5 — a rekey is just another Quick
// Mode). Requires StartDataPhase.
func (s *Session) RekeyQuickMode(espProposals []string, localIP, remoteIP net.IP) (*QuickModeResult, error) {
	return s.quickMode(espProposals, localIP, remoteIP, s.controlRoundTrip, s.sendRaw)
}

// recvESPDataPhase is RecvESP once the reader owns the socket. It waits for
// as long as ctx allows: silence from the server is not an error here. An
// idle tunnel legitimately receives nothing for long stretches, and this
// used to fail after 30s of it — which tore down a healthy session. Whether
// the peer is really gone is the engine's liveness watchdog's call (it
// probes with LCP echoes), not something a blocked read can tell.
func (s *Session) recvESPDataPhase(ctx context.Context) ([]byte, error) {
	select {
	case pkt := <-s.dp.espIn:
		return pkt, nil
	case <-s.dp.done:
		return nil, fmt.Errorf("IKE/ESP socket reader stopped: %w", s.dp.getErr())
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (dp *dataPlane) setErr(err error) {
	dp.errMu.Lock()
	defer dp.errMu.Unlock()
	if dp.err == nil {
		dp.err = err
	}
}

func (dp *dataPlane) getErr() error {
	dp.errMu.Lock()
	defer dp.errMu.Unlock()
	return dp.err
}
