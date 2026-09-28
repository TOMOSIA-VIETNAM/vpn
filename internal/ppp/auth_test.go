package ppp

import (
	"bytes"
	"testing"
)

func TestGeneratePAPAuthenticateRequest(t *testing.T) {
	req := GeneratePAPAuthenticateRequest(42, "myuser", "mypassword")
	if req.Code != PAPCodeAuthenticateRequest {
		t.Errorf("got code %d, want %d", req.Code, PAPCodeAuthenticateRequest)
	}
	if req.Identifier != 42 {
		t.Errorf("got identifier %d, want 42", req.Identifier)
	}

	pkt, err := ParsePAPPacket(req.Marshal())
	if err != nil {
		t.Fatalf("ParsePAPPacket failed: %v", err)
	}
	if pkt.Code != PAPCodeAuthenticateRequest || pkt.Identifier != 42 {
		t.Errorf("parsed packet mismatch: %+v", pkt)
	}
}

func TestParsePAPMessage(t *testing.T) {
	msg := "Login ok"
	data := append([]byte{byte(len(msg))}, []byte(msg)...)
	got := ParsePAPMessage(data)
	if got != msg {
		t.Errorf("got %q, want %q", got, msg)
	}
}

func TestGenerateCHAPMD5Response(t *testing.T) {
	id := uint8(1)
	password := "secret"
	challenge := []byte("1234567890abcdef")

	resp := GenerateCHAPMD5Response(id, password, challenge)
	if len(resp) != 16 {
		t.Fatalf("expected 16-byte MD5 response, got %d bytes", len(resp))
	}

	// Deterministic verification: recomputing must match
	resp2 := GenerateCHAPMD5Response(id, password, challenge)
	if !bytes.Equal(resp, resp2) {
		t.Errorf("CHAP-MD5 response is not deterministic")
	}
}
