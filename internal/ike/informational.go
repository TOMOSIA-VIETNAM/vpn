package ike

import (
	"encoding/binary"
	"fmt"

	"vpn/internal/vpnlog"
)

// Notify message types for Dead Peer Detection, RFC 3706 §5.
const (
	notifyRUThere    = 36136
	notifyRUThereAck = 36137
)

// dpdVendorID is RFC 3706 §4's capability tag: the 16 bytes
// AFCAD71368A1F1C96B8696FC77570100. Servers only run DPD against peers that
// announced it in Main Mode.
var dpdVendorID = []byte{
	0xAF, 0xCA, 0xD7, 0x13, 0x68, 0xA1, 0xF1, 0xC9,
	0x6B, 0x86, 0x96, 0xFC, 0x77, 0x57, 0x01, 0x00,
}

// notifyBody encodes a Notify payload body (RFC 2408 §3.14): DOI, Protocol-Id,
// SPI Size, Notify Message Type, SPI, notification data.
func notifyBody(proto uint8, notifyType uint16, spi, data []byte) []byte {
	b := make([]byte, 8, 8+len(spi)+len(data))
	binary.BigEndian.PutUint32(b[0:4], DOIIPsec)
	b[4] = proto
	b[5] = uint8(len(spi))
	binary.BigEndian.PutUint16(b[6:8], notifyType)
	b = append(b, spi...)
	return append(b, data...)
}

// deleteISAKMPBody encodes a Delete payload body (RFC 2408 §3.15) naming this
// IKE SA by its cookie pair.
func (s *Session) deleteISAKMPBody() []byte {
	b := make([]byte, 8, 24)
	binary.BigEndian.PutUint32(b[0:4], DOIIPsec)
	b[4] = protoISAKMP
	b[5] = 16
	binary.BigEndian.PutUint16(b[6:8], 1)
	b = append(b, s.InitiatorSPI[:]...)
	return append(b, s.ResponderSPI[:]...)
}

// sendInformational sends a new, encrypted Informational exchange (RFC 2409
// §5.7): HDR*, HASH(1), payload. body is the chained payloads after HASH,
// the first of which has type firstType.
func (s *Session) sendInformational(firstType uint8, body []byte) error {
	if s.Keys == nil || s.phase1IV == nil {
		return fmt.Errorf("no IKE keys yet")
	}
	msgID := randomMessageID()
	bs := blockSize(s.Transform)
	hash, err := prf(s.Transform.Hash, s.Keys.SKEYIDa, append(beUint32(msgID), body...))
	if err != nil {
		return err
	}
	plain := padToBlock(append(marshalPayload(firstType, hash), body...), bs)
	iv, err := informationalIV(s.Transform.Hash, s.phase1IV, msgID, bs)
	if err != nil {
		return err
	}
	ct, err := cbcEncrypt(s.Transform, s.Keys.EncKey, iv, plain)
	if err != nil {
		return err
	}
	h := Header{
		InitiatorSPI: s.InitiatorSPI, ResponderSPI: s.ResponderSPI,
		NextPayload: PayloadHash, Version: 0x10, ExchangeType: ExchangeInformational,
		Flags: FlagEncryption, MessageID: msgID,
	}
	h.Length = uint32(headerLen + len(ct))
	return s.sendRaw(append(h.Marshal(), ct...))
}

// replyDPD answers an authenticated R-U-THERE with the matching R-U-THERE-ACK
// (RFC 3706 §5.2): same SPI and sequence number, ACK type. A peer that
// never hears back declares us dead and tears the SA down — the tunnel then
// drops for no reason we can see from the client side.
func (s *Session) replyDPD(notify []byte) {
	ack := append([]byte(nil), notify...)
	binary.BigEndian.PutUint16(ack[6:8], notifyRUThereAck)
	if err := s.sendInformational(PayloadNotify, marshalPayload(PayloadNone, ack)); err != nil {
		vpnlog.Error(stage, "DPD reply failed", vpnlog.Fields{"err": err})
		return
	}
	vpnlog.Debug(stage, "answered server DPD R-U-THERE", nil)
}

// Close tells the server to drop this IKE SA (and, with it, the child SAs)
// and releases the socket. Without the Delete, every failed or finished
// connection leaves a half-open SA on the server that it keeps probing
// until its own timeouts fire — the "server sent a message for another IKE
// SA" noise, and sessions the server still counts against the account.
// Best effort and safe to call more than once.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		if err := s.sendInformational(PayloadDelete, marshalPayload(PayloadNone, s.deleteISAKMPBody())); err == nil {
			vpnlog.Info(stage, "IKE SA delete sent", nil)
		}
		if s.Keys != nil {
			s.Keys.Zeroize()
		}
		_ = s.conn.Close()
	})
}
