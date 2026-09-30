// Package dnsmgr configures and restores DNS servers for the VPN session dynamically
// using macOS scutil DynamicStore (in-memory only).
//
// By using in-memory DynamicStore keys (State:/Network/Service/.../DNS and State:/Network/Global/DNS),
// macOS routes DNS queries to the VPN DNS servers while the tunnel is active, without writing to
// /Library/Preferences/SystemConfiguration/preferences.plist.
//
// This guarantees that if the Mac abruptly powers off, reboots, or loses power while
// connected, no stale DNS settings are ever left in macOS Network Preferences upon reboot.
package dnsmgr

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"vpn/internal/sysbin"
)

const (
	vpnServiceID = "com.tms.vpn.dns"
	dnsStateKey  = "State:/Network/Service/" + vpnServiceID + "/DNS"
	globalDNSKey = "State:/Network/Global/DNS"
)

// Snapshot is the DNS configuration state for the VPN session.
type Snapshot struct {
	Service  string
	Servers  []string
	applied  bool
	pushed   []string // the servers Apply installed, for VerifyPrimary
	TunIface string
}

// isPrivateIPv4 returns whether the given IP address is an RFC 1918 or CGNAT private IPv4 address.
func isPrivateIPv4(ipStr string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	// 10.0.0.0/8
	if ip4[0] == 10 {
		return true
	}
	// 172.16.0.0/12 (172.16.0.0 - 172.31.255.255)
	if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
		return true
	}
	// 192.168.0.0/16
	if ip4[0] == 192 && ip4[1] == 168 {
		return true
	}
	// 100.64.0.0/10 (CGNAT)
	if ip4[0] == 100 && (ip4[1]&0xc0) == 64 {
		return true
	}
	return false
}

// PrioritizeDNSServers orders private corporate DNS servers first, followed by public DNS servers,
// and deduplicates the list while preserving order.
func PrioritizeDNSServers(servers []string) []string {
	seen := make(map[string]bool)
	var privateServers, publicServers []string
	for _, s := range servers {
		s = strings.TrimSpace(s)
		if s == "" || s == "0.0.0.0" || seen[s] {
			continue
		}
		seen[s] = true
		if isPrivateIPv4(s) {
			privateServers = append(privateServers, s)
		} else {
			publicServers = append(publicServers, s)
		}
	}
	return append(privateServers, publicServers...)
}

// FlushCache purges macOS DNS cache and notifies mDNSResponder to reload immediately.
func FlushCache() {
	_ = exec.Command(sysbin.Dscacheutil, "-flushcache").Run()
	_ = exec.Command(sysbin.Killall, "-HUP", "mDNSResponder").Run()
}

// ServiceForInterface maps a BSD interface name (e.g. "en0") to the
// networksetup service name (e.g. "Wi-Fi") that controls it.
func ServiceForInterface(iface string) (string, error) {
	out, err := exec.Command(sysbin.Networksetup, "-listallhardwareports").Output()
	if err != nil {
		return "", fmt.Errorf("list hardware ports: %w", err)
	}
	var service, device string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Hardware Port:"):
			service = strings.TrimSpace(strings.TrimPrefix(line, "Hardware Port:"))
		case strings.HasPrefix(line, "Device:"):
			device = strings.TrimSpace(strings.TrimPrefix(line, "Device:"))
			if device == iface {
				return service, nil
			}
		}
	}
	return "", fmt.Errorf("no network service found for interface %s", iface)
}

// Capture reads the current DNS servers for service.
func Capture(service string) (*Snapshot, error) {
	s := &Snapshot{Service: service}
	out, err := exec.Command(sysbin.Networksetup, "-getdnsservers", service).Output()
	if err == nil {
		text := strings.TrimSpace(string(out))
		if text != "" && !strings.Contains(text, "aren't any DNS Servers") {
			s.Servers = strings.Fields(text)
		}
	}
	return s, nil
}

// FromRecorded rebuilds a Snapshot from state previously persisted to disk.
func FromRecorded(service string, servers []string, applied bool) *Snapshot {
	return &Snapshot{Service: service, Servers: servers, applied: applied}
}

// Apply sets DNS servers dynamically via scutil DynamicStore (in-memory only).
func (s *Snapshot) Apply(servers []string) error {
	ordered := PrioritizeDNSServers(servers)
	if len(ordered) == 0 {
		return nil
	}
	serverList := strings.Join(ordered, " ")
	s.pushed = ordered

	var script strings.Builder
	// Service-level DNS entry (bound to the utun interface with priority order)
	script.WriteString("d.init\n")
	script.WriteString(fmt.Sprintf("d.add ServerAddresses * %s\n", serverList))
	script.WriteString("d.add SupplementalMatchDomains * \"\"\n")
	script.WriteString("d.add SupplementalMatchOrders * 100000\n")
	if s.TunIface != "" {
		script.WriteString(fmt.Sprintf("d.add InterfaceName %s\n", s.TunIface))
	}
	script.WriteString(fmt.Sprintf("set %s\n", dnsStateKey))

	cmd := exec.Command(sysbin.Scutil)
	cmd.Stdin = strings.NewReader(script.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("apply dynamic DNS via scutil: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	s.applied = true

	// Purge stale negative DNS cache immediately so private hosts resolve on first try
	FlushCache()
	return nil
}

// Applied returns the DNS servers Apply installed.
func (s *Snapshot) Applied() []string { return s.pushed }

// Restore removes the dynamic DNS keys: the entry Apply added and the computed
// global key, which makes macOS rebuild its resolver list from the physical
// service at once. Both live only in the in-memory DynamicStore. It never
// touches networksetup, so DNS servers the user set on the physical service
// (1.1.1.1, AdGuard, a corporate resolver) survive a disconnect and a `repair`.
func (s *Snapshot) Restore() error {
	cmd := exec.Command(sysbin.Scutil)
	cmd.Stdin = strings.NewReader(restoreScript())
	_ = cmd.Run()

	// Flush cache after removing VPN DNS keys so macOS reverts immediately to physical interface DNS
	FlushCache()
	return nil
}

// restoreScript is the scutil input Restore runs.
func restoreScript() string {
	return fmt.Sprintf("remove %s\nremove %s\n", dnsStateKey, globalDNSKey)
}

// defaultResolverServers returns the nameservers of the resolver macOS uses
// for an ordinary name: the first "resolver #N" block of `scutil --dns` that
// has nameservers and is not tied to one domain (the mDNS and reverse-lookup
// entries are). scutil lists resolvers in the order they are consulted, so
// this does not depend on which optional fields (order, if_index) a given
// macOS version prints. Only the main section is read, not the per-interface
// "for scoped queries" one.
func defaultResolverServers(out string) []string {
	var servers []string
	domain, inBlock := false, false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "DNS configuration (for scoped queries)"):
			if !domain {
				return servers
			}
			return nil
		case strings.HasPrefix(line, "resolver #"):
			if inBlock && !domain && len(servers) > 0 {
				return servers
			}
			servers, domain, inBlock = nil, false, true
		case !inBlock:
		case strings.HasPrefix(line, "domain"):
			domain = true
		case strings.HasPrefix(line, "nameserver["):
			if _, v, ok := strings.Cut(line, ":"); ok {
				servers = append(servers, strings.TrimSpace(v))
			}
		}
	}
	if inBlock && !domain {
		return servers
	}
	return nil
}

// answersFirst reports whether the resolver macOS asks first for an ordinary
// name is one of pushed. If it is not, lookups go to the physical network's
// DNS and are visible to it — a DNS leak.
func answersFirst(out string, pushed []string) bool {
	for _, s := range defaultResolverServers(out) {
		for _, p := range pushed {
			if s == p {
				return true
			}
		}
	}
	return false
}

// VerifyPrimary reports whether the pushed DNS servers are the ones macOS asks
// first, retrying briefly because configd publishes the new entry
// asynchronously. An error means the check itself could not run, which callers
// must not treat as a leak.
func VerifyPrimary(pushed []string) (bool, error) {
	var lastErr error
	for i := 0; i < 5; i++ {
		out, err := exec.Command(sysbin.Scutil, "--dns").Output()
		if err != nil {
			lastErr = err
		} else if answersFirst(string(out), pushed) {
			return true, nil
		} else {
			lastErr = nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false, lastErr
}
