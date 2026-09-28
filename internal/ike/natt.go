package ike

import (
	"crypto/md5" //nolint:gosec // required: RFC 3947 defines the NAT-T Vendor ID as literally MD5("RFC 3947"), a capability tag, not a security primitive.
	"encoding/binary"
	"net"
)

// Standard and draft NAT-Traversal Vendor IDs.
var (
	// RFC 3947 §7: MD5("RFC 3947")
	vidRFC3947 = md5Sum("RFC 3947")
	// draft-ietf-ipsec-nat-t-ike-03: MD5("draft-ietf-ipsec-nat-t-ike-03")
	vidDraft03 = md5Sum("draft-ietf-ipsec-nat-t-ike-03")
	// draft-ietf-ipsec-nat-t-ike-02\n (Windows XP/2003, Cisco IOS)
	vidDraft02N = md5Sum("draft-ietf-ipsec-nat-t-ike-02\n")
	// draft-ietf-ipsec-nat-t-ike-02
	vidDraft02 = md5Sum("draft-ietf-ipsec-nat-t-ike-02")
	// draft-ietf-ipsec-nat-t-ike-00
	vidDraft00 = md5Sum("draft-ietf-ipsec-nat-t-ike-00")
)

func md5Sum(s string) []byte {
	sum := md5.Sum([]byte(s)) //nolint:gosec
	return sum[:]
}

// RFC3947VendorID announces support for standardized NAT-Traversal.
func RFC3947VendorID() []byte {
	return vidRFC3947
}

// Draft02NVendorID announces support for draft-02\n NAT-T (used by Windows/Cisco).
func Draft02NVendorID() []byte {
	return vidDraft02N
}

// IsNATTVendorID reports whether the given Vendor ID announces NAT-T support
// under any of the standardized RFCs or draft specifications.
func IsNATTVendorID(vid []byte) bool {
	s := string(vid)
	return s == string(vidRFC3947) ||
		s == string(vidDraft03) ||
		s == string(vidDraft02N) ||
		s == string(vidDraft02) ||
		s == string(vidDraft00)
}

// computeNATD is RFC 3947 §4's NAT-D payload: HASH(CKY-I | CKY-R | Address | Port),
// using the Phase 1 hash algorithm already negotiated, over one candidate
// (address, port) pair. The initiator sends one NAT-D for its own address
// and one for what it believes the responder's address is; the responder
// does the mirror image. A mismatch on either side reveals a NAT between
// that endpoint and the peer.
func computeNATD(hashAlg int, initiatorSPI, responderSPI [8]byte, ip net.IP, port uint16) ([]byte, error) {
	ip4 := ip.To4()
	if ip4 == nil {
		ip4 = ip.To16()
	}
	buf := make([]byte, 0, 16+len(ip4)+2)
	buf = append(buf, initiatorSPI[:]...)
	buf = append(buf, responderSPI[:]...)
	buf = append(buf, ip4...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, port)
	buf = append(buf, portBytes...)
	return digest(hashAlg, buf)
}
