package ike

import (
	"encoding/hex"
	"testing"
)

// The body of a real MM2 (after the ISAKMP header), captured from a server
// that copies the initiator's transform verbatim: it chose our transform #1
// (aes256-sha256-modp2048) and kept its next-payload of 3, "another transform
// follows", though it is the proposal's only one. Parsed by the next-payload
// chain this failed as "payload chain truncated (next=3, 0 bytes left)".
const echoedTransformMM2 = "0d00003800000001000000010000002c0101000103000024010100008001000780020004800300018004000e800b0001800c7080800e0100000000144a131c81070358455c5728f20e95452f"

func TestParseChosenTransformToleratesEchoedNextPayload(t *testing.T) {
	body, err := hex.DecodeString(echoedTransformMM2)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := SplitPayloads(PayloadSA, body)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := ParseChosenTransform(payloads[0].Body)
	if err != nil {
		t.Fatalf("real MM2 rejected: %v", err)
	}
	if tr.Encryption != EncAES || tr.KeyBits != 256 || tr.Hash != HashSHA256 || tr.Group != 14 {
		t.Fatalf("got %v, want aes256-sha256-modp2048", tr)
	}
}

// The same responder habit in QM2: one ESP transform still saying "more follow".
func TestParseChosenESPSAToleratesEchoedNextPayload(t *testing.T) {
	tr, err := espProposalFor("aes128-sha1")
	if err != nil {
		t.Fatal(err)
	}
	sa, err := marshalESPSA([]Transform{tr, tr}, 0x11223344) // transform #1 carries next=3
	if err != nil {
		t.Fatal(err)
	}
	// Keep only transform #1, as the responder would, and say so in the count.
	props, err := SplitPayloads(PayloadProposal, sa[8:])
	if err != nil {
		t.Fatal(err)
	}
	prop := append([]byte(nil), props[0].Body...)
	txLen := int(prop[8+2])<<8 | int(prop[8+3])
	if prop[8] != PayloadTransform {
		t.Fatalf("fixture: transform #1 next-payload = %d, want 3", prop[8])
	}
	prop = prop[:8+txLen]
	prop[3] = 1
	chosen, err := parseChosenESPSA(saWithProposal(prop))
	if err != nil {
		t.Fatalf("QM2 with an echoed next-payload rejected: %v", err)
	}
	if chosen.SPI != 0x11223344 || chosen.Transform.Encryption != tr.Encryption {
		t.Fatalf("got %+v", chosen)
	}
}

func TestChosenTransformChecksLength(t *testing.T) {
	for _, data := range [][]byte{nil, {0, 0, 0}, {0, 0, 0, 3}, {0, 0, 0, 9, 1}} {
		if _, err := chosenTransform(data); err == nil {
			t.Fatalf("accepted %x", data)
		}
	}
	body, err := chosenTransform([]byte{3, 0, 0, 5, 0xAA, 0xBB})
	if err != nil || len(body) != 1 || body[0] != 0xAA {
		t.Fatalf("got %x, %v; want the 1-byte body, trailing bytes ignored", body, err)
	}
}
