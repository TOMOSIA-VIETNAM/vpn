package ppp

import (
	"net"
)

// IPCP option types, RFC 1332 §3 (base) + RFC 1877 (DNS extensions, which
// the reference options file relies on via `ipcp-accept-remote` picking up
// whatever the LNS supplies).
const (
	IPCPOptIPAddress    = 3
	IPCPOptPrimaryDNS   = 129
	IPCPOptSecondaryDNS = 131
)

// IPv4Option builds a 4-byte IP-address-shaped IPCP option (used for both
// OptIPAddress and the DNS options, which share the same 4-byte layout).
func IPv4Option(optType uint8, ip net.IP) Option {
	v4 := ip.To4()
	if v4 == nil {
		v4 = net.IPv4zero.To4()
	}
	return Option{Type: optType, Data: append([]byte{}, v4...)}
}

// RequestIPCPOptions builds this client's IPCP Configure-Request: requested IP
// address (or 0.0.0.0 meaning "assign me one", matching
// `ipcp-accept-local`/`ipcp-accept-remote` in the reference PPP options)
// plus a request for the LNS to supply DNS servers.
func RequestIPCPOptions(requestedIP net.IP) []Option {
	ip := net.IPv4zero
	if requestedIP != nil && !requestedIP.IsUnspecified() && requestedIP.To4() != nil {
		ip = requestedIP.To4()
	}
	return []Option{
		IPv4Option(IPCPOptIPAddress, ip),
		IPv4Option(IPCPOptPrimaryDNS, net.IPv4zero),
		IPv4Option(IPCPOptSecondaryDNS, net.IPv4zero),
	}
}

// ParseIPv4Option reads a 4-byte IP-address-shaped option's value.
func ParseIPv4Option(o Option) (net.IP, bool) {
	if len(o.Data) != 4 {
		return nil, false
	}
	return net.IPv4(o.Data[0], o.Data[1], o.Data[2], o.Data[3]), true
}

// NegotiatedIPCP is the result of a completed IPCP negotiation: our
// assigned address, the LNS's own inside address (needed to configure the
// utun interface as a point-to-point link), and whatever DNS servers the
// LNS handed out (may be none — the reference server may not push DNS at
// all, in which case the engine keeps the original resolver config,
// matching the goal's "never silently hand out public DNS the server
// didn't provide").
type NegotiatedIPCP struct {
	LocalIP    net.IP
	PeerIP     net.IP
	PrimaryDNS net.IP
	SecondDNS  net.IP
}

// ApplyOption applies one option from *our own* final, peer-acked
// Configure-Request — the values this side actually ended up using.
func (n *NegotiatedIPCP) ApplyOption(o Option) {
	ip, ok := ParseIPv4Option(o)
	if !ok {
		return
	}
	switch o.Type {
	case IPCPOptIPAddress:
		n.LocalIP = ip
	case IPCPOptPrimaryDNS:
		n.PrimaryDNS = ip
	case IPCPOptSecondaryDNS:
		n.SecondDNS = ip
	}
}

// ApplyPeerOption applies one option from the *peer's* (LNS's) own
// Configure-Request — only its IP-Address option is meaningful to us, as
// the LNS's inside address, needed to bring up the utun interface as a
// point-to-point link to it.
func (n *NegotiatedIPCP) ApplyPeerOption(o Option) {
	if o.Type != IPCPOptIPAddress {
		return
	}
	if ip, ok := ParseIPv4Option(o); ok {
		n.PeerIP = ip
	}
}

// defaultPeerIP stands in for an LNS that has no inside address of its own:
// its IPCP Configure-Request carries IP-Address 0.0.0.0, or none at all
// (seen live, answering an IPCP with no address of its own). pppd does the
// same — "make up an arbitrary address for the peer", 10.64.64.64 plus the
// unit number (ipcp.c). It only names the far end of the point-to-point
// interface: every route through the tunnel points at the interface itself.
var defaultPeerIP = net.IPv4(10, 64, 64, 64).To4()

// PointToPointPeer is the address to configure as the far end of the tunnel
// interface: the LNS's own, or defaultPeerIP when it has none (never our own
// address, which the interface would then point at itself).
func (n NegotiatedIPCP) PointToPointPeer() net.IP {
	if n.PeerIP != nil && !n.PeerIP.IsUnspecified() && !n.PeerIP.Equal(n.LocalIP) {
		return n.PeerIP
	}
	if n.LocalIP.Equal(defaultPeerIP) {
		return net.IPv4(10, 64, 64, 65).To4()
	}
	return defaultPeerIP
}
