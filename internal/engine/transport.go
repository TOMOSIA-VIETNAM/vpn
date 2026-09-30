package engine

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"syscall"

	"vpn/internal/bufpool"
	"vpn/internal/ike"
	"vpn/internal/ipsec"
	"vpn/internal/l2tp"
	"vpn/internal/ppp"
	"vpn/internal/vpnlog"
)

// espTransport implements l2tp.Transport over an ESP SA pair carried inside
// the IKE session's already-floated NAT-T socket (see ikeSessions). It reconstructs the inner
// UDP/1701 header ESP transport mode protects (RFC 4303 §3.1: the payload
// ESP encrypts is the original packet's next header onward — here, a UDP
// header plus the L2TP message) since this client never builds real IP
// packets for its own control/data traffic, only the payload IPsec expects.
type espTransport struct {
	mux         *ikeSessions // the IKE SA(s) whose socket carries the ESP
	sas         *saSet       // current pair for sending; every live pair for receiving (see rekey.go)
	repairRoute func() error
	live        *liveness // nil in tests; otherwise fed by every valid inbound packet
	l2tpPort    uint16    // our L2TP UDP port, as negotiated in Quick Mode (ike.Session.L2TPPort); 0 = 1701
	drops       atomic.Uint64
	// lastPayload is the pooled buffer behind the message Recv last returned.
	// Only the goroutine calling Recv touches it (the data plane, or the
	// negotiation before it) — see ReleaseLast.
	lastPayload []byte
	// par, while set, is the parallel receive pipeline Recv reads from (see
	// startParallel). Set and cleared by the data plane around its own Recv loop.
	par *parallelRx
}

// ReleaseLast returns the buffer of the message Recv last delivered to the
// pool. The caller must be the one that received it and must be completely
// done with it (and with every slice of it): the data plane calls this after
// writing the packet to the tunnel device. Never calling it is safe — the
// buffer is then simply garbage-collected.
func (t *espTransport) ReleaseLast() {
	bufpool.Put(t.lastPayload)
	t.lastPayload = nil
}

// noteDrop counts a discarded inbound packet and logs the first and then every
// 500th, so a burst (an old SA still being sent to, a corrupting path) leaves
// evidence without one log line per packet.
func (t *espTransport) noteDrop(msg string, err error) {
	n := t.drops.Add(1)
	if n != 1 && n%500 != 0 {
		return
	}
	f := vpnlog.Fields{"dropped_total": n}
	if err != nil {
		f["reason"] = err.Error()
	}
	if err != nil && strings.Contains(err.Error(), "already seen") {
		vpnlog.Debug("ENGINE", "duplicate packet filtered by anti-replay", f)
	} else {
		vpnlog.Warn("ENGINE", msg, f)
	}
}

const protoUDP = 17

func (t *espTransport) Send(l2tpMsg []byte) error {
	payload := append(l2tpUDPHeader(t.l2tpPort, len(l2tpMsg)), l2tpMsg...)

	pkt, err := t.sas.current().out.Encrypt(payload, protoUDP)
	if err != nil {
		return fmt.Errorf("ESP encrypt: %w", err)
	}
	if t.live != nil {
		t.live.tx.Add(1)
	}
	err = sendWithRouteRetry(func() error { return t.mux.sendESP(pkt) }, t.repairRoute)
	bufpool.Put(pkt) // WriteToUDP has copied it into the kernel
	return err
}

func (t *espTransport) SendIPFast(tunnelID, sessionID uint16, ipPkt []byte) error {
	pkt, err := t.sas.current().out.EncryptIPPacket(ourL2TPPort(t.l2tpPort), ike.L2TPPort, tunnelID, sessionID, ipPkt)
	if err != nil {
		return fmt.Errorf("ESP encrypt: %w", err)
	}
	if t.live != nil {
		t.live.tx.Add(1)
	}
	err = sendWithRouteRetry(func() error { return t.mux.sendESP(pkt) }, t.repairRoute)
	bufpool.Put(pkt)
	return err
}

// sendWithRouteRetry repairs a route lost by macOS's route reconciler and
// retries once. EHOSTUNREACH/ENETUNREACH means the kernel did not transmit
// the datagram, so the retry cannot duplicate an ESP packet.
func sendWithRouteRetry(send func() error, repairRoute func() error) error {
	err := send()
	if err == nil || repairRoute == nil || (!errors.Is(err, syscall.EHOSTUNREACH) && !errors.Is(err, syscall.ENETUNREACH)) {
		return err
	}
	if repairErr := repairRoute(); repairErr != nil {
		return fmt.Errorf("repair VPN server route after %w: %v", err, repairErr)
	}
	return send()
}

func (t *espTransport) Recv(ctx context.Context) ([]byte, error) {
	if t.par != nil {
		return t.par.recv(ctx, t)
	}
	for {
		pkt, err := t.mux.recv(ctx)
		if err != nil {
			return nil, err
		}
		if msg, ok := t.process(pkt); ok {
			return msg, nil
		}
	}
}

// process is the single-goroutine receive step: it decrypts one inbound ESP
// datagram with the SA's own state (see open) and remembers the buffer for
// ReleaseLast.
func (t *espTransport) process(pkt []byte) (msg []byte, ok bool) {
	msg, base, ok := t.open(pkt, func(sa *ipsec.SA, pkt []byte) ([]byte, byte, error) { return sa.Decrypt(pkt) })
	if ok {
		t.lastPayload = base
	}
	return msg, ok
}

// pppOverL2TP implements ppp.Transport over an established l2tp.Tunnel's
// data channel.
type pppOverL2TP struct {
	tun *l2tp.Tunnel
}

func (p *pppOverL2TP) SendIP(ipPkt []byte) error {
	pt, ps := p.tun.PeerIDs()
	if fast, ok := p.tun.Transport().(interface {
		SendIPFast(tunnelID, sessionID uint16, ipPkt []byte) error
	}); ok {
		return fast.SendIPFast(pt, ps, ipPkt)
	}
	return p.SendFrame(ppp.ProtoIP, ipPkt)
}

// releaseLast recycles the buffer of the frame RecvFrame last returned; the
// caller must not use that frame's payload afterwards.
func (p *pppOverL2TP) releaseLast() {
	if r, ok := p.tun.Transport().(interface{ ReleaseLast() }); ok {
		r.ReleaseLast()
	}
}

// startParallelRecv turns on the transport's parallel receive pipeline for the
// life of ctx where that pays off; see espTransport.startParallel for the
// contract of the returned stop function (nil when nothing was started).
func (p *pppOverL2TP) startParallelRecv(ctx context.Context) (stop func()) {
	if t, ok := p.tun.Transport().(*espTransport); ok {
		return t.startParallel(ctx)
	}
	return nil
}

func (p *pppOverL2TP) SendFrame(protocol uint16, payload []byte) error {
	frame := ppp.Frame{Protocol: protocol, Payload: payload}
	return p.tun.SendData(frame.Marshal())
}

// RecvFrame returns the next well-formed PPP frame. A malformed one is
// skipped, not returned as an error: callers treat an error here as the
// transport being gone, and one corrupt datagram must not end the session.
func (p *pppOverL2TP) RecvFrame(ctx context.Context) (uint16, []byte, error) {
	for {
		raw, err := p.tun.RecvData(ctx)
		if err != nil {
			return 0, nil, err
		}
		f, err := ppp.Parse(raw)
		if err != nil {
			vpnlog.Debug("ENGINE", "skipped malformed PPP frame", vpnlog.Fields{"err": err, "len": len(raw)})
			continue
		}
		return f.Protocol, f.Payload, nil
	}
}

// randomL2TPPort picks this connection's own L2TP port from the dynamic range
// (RFC 6335 §6). Every machine behind an office NAT reaches the server from
// the same public address, and with 1701 on all of them the server's SA
// lookup for "that address, port 1701" matches whichever of their SAs is
// newest: it then encrypts one client's SCCRP, CHAP Challenge or data with
// another's keys (reproduced live as logins that time out or are refused
// until the other machine reconnects). A port of our own gives our SA a
// selector no other client, nor an earlier session of ours, shares.
func randomL2TPPort() uint16 {
	const lo, n = 49152, 65536 - 49152
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ike.L2TPPort
	}
	return uint16(lo + int(binary.BigEndian.Uint16(b[:]))%n)
}

// l2tpPortFor is the L2TP port for the next attempt: a fresh one, unless a
// session of ours is still to be ended on the server (see staleSession).
func l2tpPortFor(stale *staleSession) uint16 {
	if stale != nil && !stale.done && stale.l2tpPort != 0 {
		return stale.l2tpPort
	}
	return randomL2TPPort()
}

// l2tpUDPHeader is the inner UDP header of an L2TP message: from our L2TP port
// (0 = 1701) to the server's 1701. The checksum stays zero: RFC 3948 §3.1.2
// permits it for integrity-protected UDP carried by ESP, since NAT changes the
// addresses a non-zero checksum's pseudo-header covers and cannot adjust it.
func l2tpUDPHeader(srcPort uint16, msgLen int) []byte {
	h := make([]byte, 8)
	binary.BigEndian.PutUint16(h[0:2], ourL2TPPort(srcPort))
	binary.BigEndian.PutUint16(h[2:4], ike.L2TPPort)
	binary.BigEndian.PutUint16(h[4:6], uint16(8+msgLen))
	return h
}

// ourL2TPPort is the inner UDP source port for every message we send, on the
// control path (l2tpUDPHeader) and the data fast path (SendIPFast) alike.
func ourL2TPPort(p uint16) uint16 {
	if p == 0 {
		return ike.L2TPPort
	}
	return p
}
