package ppp

import (
	"net"
	"testing"
)

// An LNS with no inside address of its own (IP-Address 0.0.0.0, or none)
// must still yield a usable far end for the point-to-point interface:
// "ifconfig utun 192.168.100.202 0.0.0.0" failed with "Destination address
// required" (seen live).
func TestPointToPointPeer(t *testing.T) {
	local := net.ParseIP("192.168.100.202").To4()
	for _, tc := range []struct {
		name  string
		local net.IP
		peer  net.IP
		want  string
	}{
		{"server's own address", local, net.ParseIP("192.168.100.1").To4(), "192.168.100.1"},
		{"0.0.0.0", local, net.IPv4zero.To4(), "10.64.64.64"},
		{"none", local, nil, "10.64.64.64"},
		{"our own address", local, local, "10.64.64.64"},
		{"loopback 127.0.0.1", local, net.ParseIP("127.0.0.1").To4(), "10.64.64.64"},
		{"link-local 169.254.1.1", local, net.ParseIP("169.254.1.1").To4(), "10.64.64.64"},
		{"multicast 224.0.0.1", local, net.ParseIP("224.0.0.1").To4(), "10.64.64.64"},
		{"broadcast 255.255.255.255", local, net.IPv4bcast.To4(), "10.64.64.64"},
		{"stand-in taken by us", net.ParseIP("10.64.64.64").To4(), nil, "10.64.64.65"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := NegotiatedIPCP{LocalIP: tc.local, PeerIP: tc.peer}.PointToPointPeer()
			if got.String() != tc.want {
				t.Fatalf("got %v, want %s", got, tc.want)
			}
		})
	}
}
