package ike

import "testing"

func TestWeakReasons(t *testing.T) {
	strong := Transform{Encryption: EncAES, KeyBits: 256, Hash: HashSHA256, Group: 14}
	if got := WeakReasons(strong, true); len(got) != 0 {
		t.Fatalf("AES-256/SHA-256/MODP2048 flagged as weak: %v", got)
	}
	weak := Transform{Encryption: Enc3DES, Hash: HashMD5, Group: 2}
	if got := WeakReasons(weak, true); len(got) != 3 {
		t.Fatalf("3DES/MD5/MODP1024 = %v, want 3 reasons", got)
	}
	// An ESP transform has no DH group: Group 0 must not be reported.
	if got := WeakReasons(Transform{Encryption: EncAES, Hash: HashSHA1}, false); len(got) != 0 {
		t.Fatalf("AES/SHA1 ESP flagged as weak: %v", got)
	}
}
