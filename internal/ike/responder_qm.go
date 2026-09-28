package ike

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"

	"vpn/internal/vpnlog"
)

// Server-initiated Quick Mode: the peer starts its own rekey of the ESP SAs
// and this client answers as the responder (RFC 2409 §5.5, roles swapped).
// Without it a server that rekeys first — its lifetime is shorter than the one
// we proposed, or it simply fires first — leaves us on SAs it is about to
// delete. The keying is the mirror image of EstablishQuickMode's: the same
// KEYMAT formula, with Ni the peer's nonce and Nr ours.

// qmResponderTTL is how long a half-finished responder exchange (QM2 sent,
// QM3 not yet seen) is remembered, to answer a retransmitted QM1 and to match
// the QM3.
const qmResponderTTL = 60 * time.Second

// respQM is one server-initiated exchange we have answered and await QM3 for.
type respQM struct {
	qm2     []byte // our reply, resent verbatim if QM1 is retransmitted
	ivChain []byte // CBC chain tail: the IV QM3 was encrypted with
	ni, nr  []byte
	peerSPI uint32 // the SPI the peer will send to us on... its own inbound: our outbound SPI
	mySPI   uint32
	chosen  Transform
}

// espOffer is one transform of one proposal in a peer's Quick Mode SA payload.
type espOffer struct {
	PropNum      uint8
	TransformNum uint8
	SPI          uint32
	Transform    Transform
	EncapMode    uint32
}

// parseOfferedESPSA lists every ESP transform the peer offers, in the peer's
// order of preference, plus the SA payload's situation field to echo back.
// Transforms this client cannot implement are skipped, not fatal: the peer
// commonly offers several and we only need one we support.
func parseOfferedESPSA(saBody []byte) (situation []byte, offers []espOffer, err error) {
	if len(saBody) < 8 {
		return nil, nil, fmt.Errorf("ESP SA payload too short")
	}
	situation = saBody[4:8]
	props, err := SplitPayloads(PayloadProposal, saBody[8:])
	if err != nil {
		return nil, nil, err
	}
	for _, p := range props {
		b := p.Body
		if len(b) < 8 || b[1] != protoIPsecESP || b[2] != 4 {
			continue
		}
		spi := binary.BigEndian.Uint32(b[4:8])
		txs, err := SplitPayloads(PayloadTransform, b[8:])
		if err != nil {
			continue
		}
		for _, tx := range txs {
			if len(tx.Body) < 4 {
				continue
			}
			t, mode, err := parseESPTransformBody(tx.Body)
			if err != nil {
				continue
			}
			offers = append(offers, espOffer{PropNum: b[0], TransformNum: tx.Body[0], SPI: spi, Transform: t, EncapMode: mode})
		}
	}
	return situation, offers, nil
}

// chooseESP picks the first offer that is one of our own configured
// transforms and uses the UDP-encapsulated transport mode this client speaks.
func chooseESP(offers []espOffer, ours []Transform) (espOffer, bool) {
	for _, o := range offers {
		if o.EncapMode != encapUDPTransport && o.EncapMode != encapTransport {
			continue
		}
		if offeredESP(o.Transform, ours) {
			return o, true
		}
	}
	return espOffer{}, false
}

type qmItem struct {
	typ  uint8
	body []byte
}

// chainPayloads serialises payloads with each one's next-payload field naming
// the type that follows it.
func chainPayloads(items []qmItem) []byte {
	var out []byte
	for i, it := range items {
		next := uint8(PayloadNone)
		if i+1 < len(items) {
			next = items[i+1].typ
		}
		out = append(out, marshalPayload(next, it.body)...)
	}
	return out
}

// handleQuickMode processes a Quick Mode message the peer sent that no
// exchange of ours is waiting for: either the first message of the peer's own
// rekey (QM1), or the QM3 completing one we already answered.
func (s *Session) handleQuickMode(h Header, encBody []byte) {
	ev := s.dp.events
	if len(ev.ESPProposals) == 0 || ev.NewChildSA == nil {
		vpnlog.Error(stage, "server-initiated Quick Mode (its own rekey) — not enabled; relying on client-initiated rekey", vpnlog.Fields{"msg_id": h.MessageID})
		return
	}
	s.dp.respMu.Lock()
	st := s.dp.resp[h.MessageID]
	s.dp.respMu.Unlock()
	if st != nil {
		if s.completeResponderQM(h, encBody, st) {
			return
		}
		// Not a valid QM3: the peer never saw our QM2 and retransmitted QM1.
		vpnlog.Info(stage, "server retransmitted Quick Mode QM1 — resending QM2", vpnlog.Fields{"msg_id": h.MessageID})
		if err := s.sendRaw(st.qm2); err != nil {
			vpnlog.Error(stage, "resend QM2 failed", vpnlog.Fields{"err": err})
		}
		return
	}
	s.startResponderQM(h, encBody)
}

func (s *Session) startResponderQM(h Header, encBody []byte) {
	fail := func(msg string, err error) {
		f := vpnlog.Fields{"msg_id": h.MessageID}
		if err != nil {
			f["err"] = err
		}
		vpnlog.Error(stage, "server-initiated Quick Mode rejected: "+msg, f)
	}
	payloads, plain, err := s.decryptExchange(h, encBody)
	if err != nil {
		fail("undecodable", err)
		return
	}
	if err := verifyHash1(s.Transform.Hash, s.Keys.SKEYIDa, h.MessageID, h.NextPayload, payloads, plain); err != nil {
		fail("failed authentication", err)
		return
	}

	var saBody, ni []byte
	var ids, natOAs []qmItem
	for _, p := range payloads[1:] {
		switch p.Type {
		case PayloadSA:
			saBody = p.Body
		case PayloadNonce:
			ni = p.Body
		case PayloadKE:
			fail("it asks for PFS (a Diffie-Hellman exchange), which this client does not do", nil)
			return
		case PayloadID:
			ids = append(ids, qmItem{PayloadID, p.Body})
		case payloadNATOA:
			natOAs = append(natOAs, qmItem{payloadNATOA, p.Body})
		}
	}
	if saBody == nil || ni == nil {
		fail("missing SA or Nonce", nil)
		return
	}
	situation, offers, err := parseOfferedESPSA(saBody)
	if err != nil {
		fail("unreadable SA payload", err)
		return
	}
	ours := make([]Transform, 0, len(s.dp.events.ESPProposals))
	for _, name := range s.dp.events.ESPProposals {
		t, err := espProposalFor(name)
		if err != nil {
			fail("bad local ESP proposal", err)
			return
		}
		ours = append(ours, t)
	}
	pick, ok := chooseESP(offers, ours)
	if !ok {
		fail(fmt.Sprintf("none of its %d offered transforms is one we accept", len(offers)), nil)
		return
	}

	var spiBytes [4]byte
	nr := make([]byte, 16)
	if _, err := rand.Read(spiBytes[:]); err != nil {
		fail("random SPI", err)
		return
	}
	if _, err := rand.Read(nr); err != nil {
		fail("random nonce", err)
		return
	}
	mySPI := binary.BigEndian.Uint32(spiBytes[:])

	tx, err := marshalESPTransformMode(pick.TransformNum, pick.Transform, uint16(pick.EncapMode), PayloadNone)
	if err != nil {
		fail("cannot encode our transform", err)
		return
	}
	prop := make([]byte, 8, 8+len(tx))
	prop[0], prop[1], prop[2], prop[3] = pick.PropNum, protoIPsecESP, 4, 1
	copy(prop[4:8], spiBytes[:])
	prop = append(prop, tx...)
	sa := make([]byte, 8)
	binary.BigEndian.PutUint32(sa[0:4], DOIIPsec)
	copy(sa[4:8], situation)
	sa = append(sa, marshalPayload(PayloadNone, prop)...)

	items := []qmItem{{PayloadSA, sa}, {PayloadNonce, nr}}
	items = append(items, ids...)
	items = append(items, natOAs...)
	rest := chainPayloads(items)

	// HASH(2) = prf(SKEYID_a, M-ID | Ni_b | everything after the HASH payload).
	hash2, err := prf(s.Transform.Hash, s.Keys.SKEYIDa, append(append(beUint32(h.MessageID), ni...), rest...))
	if err != nil {
		fail("HASH(2)", err)
		return
	}
	bs := blockSize(s.Transform)
	if len(encBody) < bs {
		fail("short message", nil)
		return
	}
	// QM2 continues QM1's CBC chain: its IV is QM1's last ciphertext block.
	ct, err := cbcEncrypt(s.Transform, s.Keys.EncKey, encBody[len(encBody)-bs:], padToBlock(append(marshalPayload(PayloadSA, hash2), rest...), bs))
	if err != nil {
		fail("encrypt QM2", err)
		return
	}
	hdr := Header{InitiatorSPI: s.InitiatorSPI, ResponderSPI: s.ResponderSPI, NextPayload: PayloadHash, Version: 0x10, ExchangeType: ExchangeQuickMode, Flags: FlagEncryption, MessageID: h.MessageID}
	hdr.Length = uint32(headerLen + len(ct))
	qm2 := append(hdr.Marshal(), ct...)

	st := &respQM{qm2: qm2, ivChain: append([]byte(nil), ct[len(ct)-bs:]...), ni: ni, nr: nr, peerSPI: pick.SPI, mySPI: mySPI, chosen: pick.Transform}
	s.dp.respMu.Lock()
	s.dp.resp[h.MessageID] = st
	s.dp.respMu.Unlock()
	time.AfterFunc(qmResponderTTL, func() {
		s.dp.respMu.Lock()
		delete(s.dp.resp, h.MessageID)
		s.dp.respMu.Unlock()
	})
	if err := s.sendRaw(qm2); err != nil {
		fail("send QM2", err)
		return
	}
	vpnlog.Info(stage, "server-initiated Quick Mode: QM2 sent", vpnlog.Fields{
		"msg_id": h.MessageID, "encryption": pick.Transform.Encryption, "hash": pick.Transform.Hash,
	})
}

// completeResponderQM verifies QM3 and, if genuine, derives the keys and
// hands the new SA pair to the engine. Reports whether msg was that QM3.
func (s *Session) completeResponderQM(h Header, encBody []byte, st *respQM) bool {
	bs := blockSize(s.Transform)
	if len(encBody) == 0 || len(encBody)%bs != 0 {
		return false
	}
	plain, err := cbcDecrypt(s.Transform, s.Keys.EncKey, st.ivChain, encBody)
	if err != nil {
		return false
	}
	payloads, err := SplitPayloads(h.NextPayload, plain)
	if err != nil || len(payloads) == 0 || payloads[0].Type != PayloadHash {
		return false
	}
	want, err := quickModeHash3(s.Transform.Hash, s.Keys.SKEYIDa, h.MessageID, st.ni, st.nr)
	if err != nil || !hmac.Equal(want, payloads[0].Body) {
		return false
	}

	encLen, authLen := espKeyLens(st.chosen)
	// Keys for what we receive use our SPI; for what we send, the peer's.
	inKeymat, err := quickModeKeymat(s.Transform.Hash, s.Keys.SKEYIDd, protoIPsecESP, st.mySPI, st.ni, st.nr, encLen+authLen)
	if err != nil {
		vpnlog.Error(stage, "server-initiated Quick Mode: key derivation failed", vpnlog.Fields{"err": err})
		return true
	}
	outKeymat, err := quickModeKeymat(s.Transform.Hash, s.Keys.SKEYIDd, protoIPsecESP, st.peerSPI, st.ni, st.nr, encLen+authLen)
	if err != nil {
		vpnlog.Error(stage, "server-initiated Quick Mode: key derivation failed", vpnlog.Fields{"err": err})
		return true
	}
	res := &QuickModeResult{
		Inbound:  ChildSA{SPI: st.mySPI, EncKey: inKeymat[:encLen], AuthKey: inKeymat[encLen:], Transform: st.chosen},
		Outbound: ChildSA{SPI: st.peerSPI, EncKey: outKeymat[:encLen], AuthKey: outKeymat[encLen:], Transform: st.chosen},
		Lifetime: effectiveLifetime(espLifetime, st.chosen.LifeSecs),
	}
	s.dp.respMu.Lock()
	delete(s.dp.resp, h.MessageID)
	s.dp.respMu.Unlock()
	vpnlog.Info(stage, "server-initiated Quick Mode ESTABLISHED", vpnlog.Fields{
		"in_spi": fmt.Sprintf("%08x", res.Inbound.SPI), "out_spi": fmt.Sprintf("%08x", res.Outbound.SPI), "lifetime_s": int(res.Lifetime / time.Second),
	})
	s.dp.events.NewChildSA(res)
	return true
}
