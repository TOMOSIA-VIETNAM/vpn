package ppp

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeLNS plays the authenticator side of MS-CHAPv2 over the Transport
// interface: it sends one Challenge, then answers the client's Response
// with whatever Success message successMsg builds from it.
type fakeLNS struct {
	challenge  []byte
	successMsg func(resp []byte) string
	inbox      chan fakeFrame // frames queued for the client
}

type fakeFrame struct {
	protocol uint16
	payload  []byte
}

func newFakeLNS(successMsg func(resp []byte) string) *fakeLNS {
	f := &fakeLNS{
		challenge:  []byte("0123456789abcdef"),
		successMsg: successMsg,
		inbox:      make(chan fakeFrame, 4),
	}
	ch := CHAPPacket{Code: CHAPCodeChallenge, Identifier: 7, Value: f.challenge, Name: []byte("lns")}
	f.inbox <- fakeFrame{ProtoCHAP, ch.Marshal()}
	return f
}

func (f *fakeLNS) SendFrame(protocol uint16, payload []byte) error {
	if protocol != ProtoCHAP {
		return nil
	}
	pkt, err := ParseCHAPPacket(payload)
	if err != nil || pkt.Code != CHAPCodeResponse {
		return err
	}
	ok := CHAPPacket{Code: CHAPCodeSuccess, Identifier: pkt.Identifier, Message: []byte(f.successMsg(pkt.Value))}
	f.inbox <- fakeFrame{ProtoCHAP, ok.Marshal()}
	return nil
}

func (f *fakeLNS) RecvFrame(ctx context.Context) (uint16, []byte, error) {
	select {
	case fr := <-f.inbox:
		return fr.protocol, fr.payload, nil
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

// genuineSuccess computes S= exactly as an LNS that knows password would.
func genuineSuccess(challenge []byte, username, password string) func([]byte) string {
	return func(resp []byte) string {
		peerChallenge, ntResponse := resp[0:16], resp[24:48]
		ch, _ := challengeHash(peerChallenge, challenge, username)
		pwHash, _ := ntPasswordHash(password)
		auth := authenticatorResponse(pwHash, ntResponse, ch)
		return fmt.Sprintf("S=%s M=Access granted", strings.ToUpper(hex.EncodeToString(auth)))
	}
}

func runAuthAgainst(t *testing.T, lns Transport) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return runAuth(ctx, lns, "alice", "correct horse", 0, AuthProtocol{Protocol: ProtoCHAP, Algorithm: CHAPAlgoMSCHAPv2})
}

func TestRunAuthAcceptsGenuineAuthenticator(t *testing.T) {
	lns := newFakeLNS(nil)
	lns.successMsg = genuineSuccess(lns.challenge, "alice", "correct horse")
	if err := runAuthAgainst(t, lns); err != nil {
		t.Fatalf("genuine LNS rejected: %v", err)
	}
}

// An impersonator that holds only the PSK can reach the PPP stage but
// cannot compute S= without the account's NT password hash.
func TestRunAuthRejectsImpersonator(t *testing.T) {
	challenge := newFakeLNS(nil).challenge
	cases := map[string]func([]byte) string{
		"no S= at all":        func([]byte) string { return "M=Welcome" },
		"S= for wrong secret": genuineSuccess(challenge, "alice", "guessed password"),
		"empty message":       func([]byte) string { return "" },
	}
	for name, msg := range cases {
		lns := newFakeLNS(msg)
		if err := runAuthAgainst(t, lns); err == nil {
			t.Fatalf("%s: impersonating LNS was accepted", name)
		}
	}
}

// fakeRetransmittingLNS sends the Challenge twice (as after a lost Response)
// and answers with the Success computed for the client's FIRST Response,
// the way pppd resends its cached Success.
type fakeRetransmittingLNS struct {
	*fakeLNS
	responses [][]byte
}

func (f *fakeRetransmittingLNS) SendFrame(protocol uint16, payload []byte) error {
	pkt, err := ParseCHAPPacket(payload)
	if err != nil || pkt.Code != CHAPCodeResponse {
		return err
	}
	f.responses = append(f.responses, append([]byte{}, pkt.Value...))
	if len(f.responses) == 1 {
		ch := CHAPPacket{Code: CHAPCodeChallenge, Identifier: 7, Value: f.challenge, Name: []byte("lns")}
		f.inbox <- fakeFrame{ProtoCHAP, ch.Marshal()}
		return nil
	}
	ok := CHAPPacket{Code: CHAPCodeSuccess, Identifier: pkt.Identifier, Message: []byte(f.successMsg(f.responses[0]))}
	f.inbox <- fakeFrame{ProtoCHAP, ok.Marshal()}
	return nil
}

func TestRunAuthAcceptsSuccessForEarlierResponse(t *testing.T) {
	base := newFakeLNS(nil)
	base.successMsg = genuineSuccess(base.challenge, "alice", "correct horse")
	lns := &fakeRetransmittingLNS{fakeLNS: base}
	if err := runAuthAgainst(t, lns); err != nil {
		t.Fatalf("Success for the first of two Responses rejected: %v", err)
	}
	if len(lns.responses) != 2 {
		t.Fatalf("expected two Responses, got %d", len(lns.responses))
	}
}
