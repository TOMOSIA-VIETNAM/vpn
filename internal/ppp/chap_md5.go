// Package ppp implements CHAP-MD5 (RFC 1994) authentication.
package ppp

import (
	"crypto/md5" //nolint:gosec // required: RFC 1994 defines CHAP-MD5 as MD5(ID | Secret | Challenge).
)

// GenerateCHAPMD5Response computes RFC 1994 §2's CHAP-MD5 response value:
//
//	Response = MD5(Identifier | Password | Challenge)
func GenerateCHAPMD5Response(identifier uint8, password string, challenge []byte) []byte {
	h := md5.New() //nolint:gosec
	h.Write([]byte{identifier})
	h.Write([]byte(password))
	h.Write(challenge)
	return h.Sum(nil)
}
