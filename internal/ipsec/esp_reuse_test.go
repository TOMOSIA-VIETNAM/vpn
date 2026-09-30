package ipsec

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"testing"

	"vpn/internal/bufpool"
)

// The SA reuses one CBC mode and re-keys it per packet (SetIV). Every packet —
// not just the first — must come out exactly as a freshly built standard-library
// CBC mode would produce it, and recycling buffers between packets must not
// corrupt a later one.
func TestReusedCBCMatchesFreshCipherAcrossPackets(t *testing.T) {
	for _, s := range suites {
		t.Run(s.String(), func(t *testing.T) {
			out, in := s.pair(t)
			enc := bytes.Repeat([]byte{0x11}, s.encLen)
			var block cipher.Block
			if s.cipher == Cipher3DESCBC {
				block, _ = des.NewTripleDESCipher(enc)
			} else {
				block, _ = aes.NewCipher(enc)
			}
			bl := block.BlockSize()
			for i := 0; i < 50; i++ {
				payload := bytes.Repeat([]byte{byte(i + 1)}, 40+i*27)
				pkt, err := out.Encrypt(payload, 17)
				if err != nil {
					t.Fatal(err)
				}
				// Independent decryption with a fresh mode.
				ct := pkt[8+bl : len(pkt)-s.icvLen]
				plain := make([]byte, len(ct))
				cipher.NewCBCDecrypter(block, pkt[8:8+bl]).CryptBlocks(plain, ct)
				if !bytes.HasPrefix(plain, payload) {
					t.Fatalf("packet %d: reused encrypter diverges from a fresh CBC mode", i)
				}
				got, nh, err := in.Decrypt(pkt)
				if err != nil || nh != 17 || !bytes.Equal(got, payload) {
					t.Fatalf("packet %d: Decrypt = %x.. nh=%d err=%v", i, got[:min(4, len(got))], nh, err)
				}
				// What the engine does once each buffer has been consumed.
				bufpool.Put(pkt)
				bufpool.Put(got)
			}
		})
	}
}

func BenchmarkESPRoundTrip(b *testing.B) {
	for _, s := range suites {
		b.Run(s.String(), func(b *testing.B) {
			out, in := s.pair(b)
			ip := make([]byte, 1280)
			b.SetBytes(int64(len(ip)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				pkt, err := out.EncryptIPPacket(1701, 1701, 1, 1, ip)
				if err != nil {
					b.Fatal(err)
				}
				plain, _, err := in.Decrypt(pkt)
				if err != nil {
					b.Fatal(err)
				}
				bufpool.Put(pkt)
				bufpool.Put(plain)
			}
		})
	}
}
