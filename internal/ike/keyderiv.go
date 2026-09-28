package ike

// Phase1Keys holds the SKEYID family derived per RFC 2409 §5 for
// pre-shared-key authentication, plus the per-transform encryption key
// used for every encrypted Phase 1/Quick Mode message from here on.
type Phase1Keys struct {
	HashAlg   int
	SKEYID    []byte
	SKEYIDd   []byte // used to derive Phase 2 (Quick Mode / IPsec) keys
	SKEYIDa   []byte // authenticates subsequent exchanges (not used further here — PSK auth uses HASH payloads keyed by SKEYID, RFC2409 §5)
	SKEYIDe   []byte // seeds the Phase 1 encryption key
	EncKey    []byte // expanded to the negotiated cipher's key length
	Transform Transform
}

// DerivePhase1Keys computes SKEYID/SKEYID_d/a/e and the encryption key from
// the PSK, the two nonces, the DH shared secret, and the two ISAKMP cookies
// — RFC 2409 §5's exact PSK formulas:
//
//	SKEYID     = prf(pre-shared-key, Ni_b | Nr_b)
//	SKEYID_d   = prf(SKEYID, g^xy | CKY-I | CKY-R | 0)
//	SKEYID_a   = prf(SKEYID, SKEYID_d | g^xy | CKY-I | CKY-R | 1)
//	SKEYID_e   = prf(SKEYID, SKEYID_a | g^xy | CKY-I | CKY-R | 2)
func DerivePhase1Keys(t Transform, psk, ni, nr, gxy []byte, ckyI, ckyR [8]byte) (*Phase1Keys, error) {
	skeyid, err := prf(t.Hash, psk, append(append([]byte{}, ni...), nr...))
	if err != nil {
		return nil, err
	}

	base := append(append([]byte{}, gxy...), ckyI[:]...)
	base = append(base, ckyR[:]...)

	skeyidD, err := prf(t.Hash, skeyid, append(append([]byte{}, base...), 0))
	if err != nil {
		return nil, err
	}
	skeyidA, err := prf(t.Hash, skeyid, append(append(append([]byte{}, skeyidD...), base...), 1))
	if err != nil {
		return nil, err
	}
	skeyidE, err := prf(t.Hash, skeyid, append(append(append([]byte{}, skeyidA...), base...), 2))
	if err != nil {
		return nil, err
	}

	encKey, err := expandKey(t.Hash, skeyidE, cipherKeyLen(t))
	if err != nil {
		return nil, err
	}

	return &Phase1Keys{
		HashAlg:   t.Hash,
		SKEYID:    skeyid,
		SKEYIDd:   skeyidD,
		SKEYIDa:   skeyidA,
		SKEYIDe:   skeyidE,
		EncKey:    encKey,
		Transform: t,
	}, nil
}

// ComputeHashI is RFC 2409 §5's PSK Main Mode identity hash:
//
//	HASH_I = prf(SKEYID, g^xi | g^xr | CKY-I | CKY-R | SAi_b | IDii_b)
func (k *Phase1Keys) ComputeHashI(gxi, gxr []byte, ckyI, ckyR [8]byte, saBody, idBody []byte) ([]byte, error) {
	return k.computeHash(gxi, gxr, ckyI, ckyR, saBody, idBody)
}

// ComputeHashR is the mirror image for the responder's identity:
//
//	HASH_R = prf(SKEYID, g^xr | g^xi | CKY-R | CKY-I | SAi_b | IDir_b)
func (k *Phase1Keys) ComputeHashR(gxr, gxi []byte, ckyR, ckyI [8]byte, saBody, idBody []byte) ([]byte, error) {
	return k.computeHash(gxr, gxi, ckyR, ckyI, saBody, idBody)
}

func (k *Phase1Keys) computeHash(a, b []byte, cky1, cky2 [8]byte, saBody, idBody []byte) ([]byte, error) {
	data := append([]byte{}, a...)
	data = append(data, b...)
	data = append(data, cky1[:]...)
	data = append(data, cky2[:]...)
	data = append(data, saBody...)
	data = append(data, idBody...)
	return prf(k.HashAlg, k.SKEYID, data)
}

// Zeroize clears all sensitive keying material from memory.
func (k *Phase1Keys) Zeroize() {
	if k == nil {
		return
	}
	for i := range k.SKEYID {
		k.SKEYID[i] = 0
	}
	for i := range k.SKEYIDd {
		k.SKEYIDd[i] = 0
	}
	for i := range k.SKEYIDa {
		k.SKEYIDa[i] = 0
	}
	for i := range k.SKEYIDe {
		k.SKEYIDe[i] = 0
	}
	for i := range k.EncKey {
		k.EncKey[i] = 0
	}
}
