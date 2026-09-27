package ipsec

import "testing"

// In-order 1400-byte packets through Decrypt: the steady state of a busy tunnel.
func BenchmarkESPDecryptInOrder(b *testing.B) {
	out, in := suites[1].pair(b)
	payload := make([]byte, 1400)
	pkts := make([][]byte, b.N)
	for i := range pkts {
		p, err := out.Encrypt(payload, 17)
		if err != nil {
			b.Fatal(err)
		}
		pkts[i] = p
	}
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for _, p := range pkts {
		if _, _, err := in.Decrypt(p); err != nil {
			b.Fatal(err)
		}
	}
}
