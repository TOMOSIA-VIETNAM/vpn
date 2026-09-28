package routing

import (
	"strings"
	"testing"
)

// Watch must never remove a route it keeps alive: a delete, even one
// immediately followed by an add, leaks traffic outside the tunnel.
func TestReassertOnlyAdds(t *testing.T) {
	s := &Snapshot{
		DefaultGateway: "192.0.2.1",
		VPNServerIP:    "203.0.113.7",
		hostRouteAdded: true,
		overrideAdded:  true,
		tunIface:       "utun9",
	}
	cmds := s.reassertCommands()
	if len(cmds) != 1+len(ipv4OverrideNets)+len(ipv6RejectNets) {
		t.Fatalf("got %d commands, want every managed route: %q", len(cmds), cmds)
	}
	var joined []string
	for _, c := range cmds {
		if c[1] != "add" {
			t.Fatalf("reassert issued a non-add command: %q", c)
		}
		joined = append(joined, strings.Join(c, " "))
	}
	all := strings.Join(joined, "\n")
	for _, want := range []string{
		"-n add -static -host 203.0.113.7 192.0.2.1",
		"-n add -static -net 0.0.0.0/1 -interface utun9",
		"-n add -static -net 128.0.0.0/1 -interface utun9",
		"-n add -inet6 -net :: -prefixlen 1 ::1 -reject -static",
		"-n add -inet6 -net 8000:: -prefixlen 1 ::1 -reject -static",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
}

func TestReassertNothingBeforeSetup(t *testing.T) {
	if cmds := (&Snapshot{}).reassertCommands(); len(cmds) != 0 {
		t.Fatalf("unconfigured snapshot reasserted %q", cmds)
	}
}

func TestReassertIncludesSplitTunnelHosts(t *testing.T) {
	s := &Snapshot{tunIface: "utun9", tunnelHosts: []string{"10.8.0.1"}}
	cmds := s.reassertCommands()
	if len(cmds) != 1 || strings.Join(cmds[0], " ") != "-n add -static -host 10.8.0.1 -interface utun9" {
		t.Fatalf("got %q", cmds)
	}
}

// The kill switch swaps the tunnel's /1 routes for blackholes and back in one
// step; a delete-then-add anywhere in that path would leak traffic.
func TestBlackholeArgs(t *testing.T) {
	cases := map[string][]string{
		"-n change -net 0.0.0.0/1 127.0.0.1 -blackhole":        ipv4BlackholeChangeArgs("0.0.0.0/1"),
		"-n add -static -net 128.0.0.0/1 127.0.0.1 -blackhole": ipv4BlackholeAddArgs("128.0.0.0/1"),
		"-n change -net 0.0.0.0/1 -interface utun9":            ipv4OverrideChangeArgs("0.0.0.0/1", "utun9"),
	}
	for want, got := range cases {
		if strings.Join(got, " ") != want {
			t.Errorf("got %q want %q", strings.Join(got, " "), want)
		}
	}
}

func TestRestoreKeepingBlackholeSkipsOverrides(t *testing.T) {
	// Nothing to remove and nothing installed: it must not touch the /1 halves,
	// and must not fail — exercised without root because every removal is
	// conditional on state this snapshot never acquired.
	s := &Snapshot{overrideAdded: true}
	if err := s.RestoreKeepingBlackhole(); err != nil {
		t.Fatal(err)
	}
}

func TestP2PInterfaceArgs(t *testing.T) {
	tests := []struct {
		name     string
		iface    string
		local    string
		peer     string
		mtu      int
		wantArgs string
	}{
		{
			name:     "valid peer provided",
			iface:    "utun0",
			local:    "192.168.100.205",
			peer:     "192.168.100.1",
			mtu:      1400,
			wantArgs: "utun0 inet 192.168.100.205 192.168.100.1 netmask 255.255.255.255 mtu 1400 up",
		},
		{
			name:     "peer is empty string",
			iface:    "utun0",
			local:    "192.168.100.205",
			peer:     "",
			mtu:      1400,
			wantArgs: "utun0 inet 192.168.100.205 10.64.64.64 netmask 255.255.255.255 mtu 1400 up",
		},
		{
			name:     "peer is 0.0.0.0",
			iface:    "utun0",
			local:    "192.168.100.205",
			peer:     "0.0.0.0",
			mtu:      1400,
			wantArgs: "utun0 inet 192.168.100.205 10.64.64.64 netmask 255.255.255.255 mtu 1400 up",
		},
		{
			name:     "peer equals local IP",
			iface:    "utun0",
			local:    "192.168.100.205",
			peer:     "192.168.100.205",
			mtu:      1400,
			wantArgs: "utun0 inet 192.168.100.205 10.64.64.64 netmask 255.255.255.255 mtu 1400 up",
		},
		{
			name:     "local is defaultPeerIP and peer empty",
			iface:    "utun0",
			local:    "10.64.64.64",
			peer:     "",
			mtu:      1280,
			wantArgs: "utun0 inet 10.64.64.64 10.64.64.65 netmask 255.255.255.255 mtu 1280 up",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(p2pInterfaceArgs(tt.iface, tt.local, tt.peer, tt.mtu), " ")
			if got != tt.wantArgs {
				t.Fatalf("got %q, want %q", got, tt.wantArgs)
			}
		})
	}
}
