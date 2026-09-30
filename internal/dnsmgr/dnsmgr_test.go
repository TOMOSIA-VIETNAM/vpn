package dnsmgr

import (
	"reflect"
	"strings"
	"testing"
)

func TestIsPrivateIPv4(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.254", true},
		{"172.16.0.1", true},
		{"172.31.255.254", true},
		{"172.32.0.1", false},
		{"192.168.1.1", true},
		{"192.168.100.1", true},
		{"100.64.0.1", true},
		{"100.127.255.254", true},
		{"100.128.0.1", false},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"118.238.201.33", false},
		{"invalid-ip", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := isPrivateIPv4(tt.ip); got != tt.want {
				t.Errorf("isPrivateIPv4(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestPrioritizeDNSServers(t *testing.T) {
	tests := []struct {
		name    string
		servers []string
		want    []string
	}{
		{
			name:    "public before private",
			servers: []string{"118.238.201.33", "192.168.100.1"},
			want:    []string{"192.168.100.1", "118.238.201.33"},
		},
		{
			name:    "already prioritized with duplicates and invalid",
			servers: []string{"192.168.100.1", "10.0.0.1", "118.238.201.33", "192.168.100.1", "", "0.0.0.0", "8.8.8.8"},
			want:    []string{"192.168.100.1", "10.0.0.1", "118.238.201.33", "8.8.8.8"},
		},
		{
			name:    "all private",
			servers: []string{"10.1.2.3", "192.168.1.1"},
			want:    []string{"10.1.2.3", "192.168.1.1"},
		},
		{
			name:    "all public",
			servers: []string{"8.8.8.8", "1.1.1.1"},
			want:    []string{"8.8.8.8", "1.1.1.1"},
		},
		{
			name:    "empty",
			servers: []string{},
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrioritizeDNSServers(tt.servers)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PrioritizeDNSServers(%v) = %v, want %v", tt.servers, got, tt.want)
			}
		})
	}
}

// Restore must remove only this client's own DynamicStore key: never the
// macOS-owned global DNS key, and never anything via networksetup (which used
// to reset the user's own DNS servers to "Empty").
func TestRestoreScriptTouchesOnlyOwnKey(t *testing.T) {
	got := restoreScript()
	want := "remove State:/Network/Service/com.tms.vpn.dns/DNS\n"
	if got != want {
		t.Fatalf("restoreScript() = %q, want %q", got, want)
	}
}

// Real `scutil --dns` output, captured on a Mac with the VPN connected: the
// VPN's resolver (order 101400) comes before the Wi-Fi resolver (200000).
const dnsConnected = `DNS configuration

resolver #1
  search domain[0] : home.arpa
  nameserver[0] : 10.200.110.1
  flags    : Supplemental, Request A records
  reach    : 0x00000002 (Reachable)
  order    : 101400

resolver #2
  nameserver[0] : 8.8.8.8
  nameserver[1] : 8.8.4.4
  nameserver[2] : 1.1.1.1
  if_index : 6 (en0)
  flags    : Request A records
  reach    : 0x00000000 (Not Reachable)
  order    : 200000

resolver #3
  domain   : local
  options  : mdns
  timeout  : 5
  flags    : Request A records
  order    : 300000

DNS configuration (for scoped queries)

resolver #1
  nameserver[0] : 8.8.8.8
  if_index : 6 (en0)
  order    : 1
`

const dnsDisconnected = `DNS configuration

resolver #1
  search domain[0] : home.arpa
  nameserver[0] : 8.8.8.8
  nameserver[1] : 8.8.4.4
  if_index : 6 (en0)
  flags    : Request A records

resolver #2
  domain   : local
  order    : 300000
`

func TestAnswersFirst(t *testing.T) {
	if !answersFirst(dnsConnected, []string{"10.200.110.1"}) {
		t.Fatal("the VPN resolver is first in real connected output, but answersFirst said no")
	}
	if answersFirst(dnsConnected, []string{"10.9.9.9"}) {
		t.Fatal("reported a server that is not in the first resolver")
	}
	// Without the VPN entry the physical resolver answers first: that is the leak.
	if answersFirst(dnsDisconnected, []string{"10.200.110.1"}) {
		t.Fatal("reported no leak although the VPN resolver is absent")
	}
	// A VPN resolver that macOS prints with an if_index line is still the default one.
	scoped := strings.Replace(dnsConnected, "  nameserver[0] : 10.200.110.1\n", "  nameserver[0] : 10.200.110.1\n  if_index : 22 (utun4)\n", 1)
	if !answersFirst(scoped, []string{"10.200.110.1"}) {
		t.Fatal("an interface-bound VPN resolver listed first was reported as a leak")
	}
	if answersFirst("", []string{"10.200.110.1"}) {
		t.Fatal("empty output must not count as verified")
	}
}
