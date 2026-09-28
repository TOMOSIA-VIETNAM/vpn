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

// Restore removes the dynamic scutil DNS keys and cleans any legacy networksetup DNS.
func (s *Snapshot) Restore() error {
	var script strings.Builder
	script.WriteString(fmt.Sprintf("remove %s\n", dnsStateKey))
	script.WriteString(fmt.Sprintf("remove %s\n", globalDNSKey))

	cmd := exec.Command(sysbin.Scutil)
	cmd.Stdin = strings.NewReader(script.String())
	_ = cmd.Run()

	// Flush cache after removing VPN DNS keys so macOS reverts immediately to physical interface DNS
	FlushCache()

	// Clean up any legacy persistent DNS left on the Wi-Fi/Ethernet service by previous versions
	if s.Service != "" {
		out, err := exec.Command(sysbin.Networksetup, "-getdnsservers", s.Service).Output()
		if err == nil {
			text := strings.TrimSpace(string(out))
			if text != "" && !strings.Contains(text, "aren't any DNS Servers") {
				_ = exec.Command(sysbin.Networksetup, "-setdnsservers", s.Service, "Empty").Run()
			}
		}
	}
	return nil
}

// CleanPersistentSettings removes any leftover persistent DNS settings from all network services.
func CleanPersistentSettings() {
	out, err := exec.Command(sysbin.Networksetup, "-listallnetworkservices").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		svc := strings.TrimSpace(line)
		if svc == "" || strings.Contains(svc, "*") {
			continue
		}
		cur, err := exec.Command(sysbin.Networksetup, "-getdnsservers", svc).Output()
		if err == nil {
			text := strings.TrimSpace(string(cur))
			if text != "" && !strings.Contains(text, "aren't any DNS Servers") {
				_ = exec.Command(sysbin.Networksetup, "-setdnsservers", svc, "Empty").Run()
			}
		}
	}
	FlushCache()
}
