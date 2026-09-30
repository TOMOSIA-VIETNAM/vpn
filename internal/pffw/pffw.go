// Package pffw is the packet-filter half of the kill switch.
//
// The kill switch's first half is routing (see routing.Blackhole): while a
// full tunnel reconnects, the two /1 routes drop everything instead of letting
// it fall back to the physical network. Routes have one gap: a socket an
// application has bound to a specific interface (IP_BOUND_IF — what "use Wi-Fi
// for this connection" does) is routed by that interface's own scoped default
// route and never consults the /1 routes. pf filters by packet, whatever route
// it took, so it closes that gap.
//
// The rules live in a sub-anchor of "com.apple/*", which macOS's stock
// /etc/pf.conf already evaluates — so nothing in the main ruleset, and no file
// on disk, is touched, and a reboot clears everything. If the user runs a
// custom pf.conf without that anchor the rules simply have no effect and the
// routing half still holds; every failure here is reported to the caller, who
// logs it and carries on.
//
// Everything in this package must run as root (privilege.Elevate).
package pffw

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"vpn/internal/sysbin"
)

// Anchor holds the rules. The number sorts it before Apple's own sub-anchors
// (200.AirDrop, 250.ApplicationFirewall), so their "pass quick" rules cannot
// let through what these block.
const Anchor = "com.apple/100.TomosiaVPN"

// tokenFile remembers the reference `pfctl -E` handed out, so Disable can
// release exactly that one and leave pf enabled for anyone else using it.
var tokenFile = "/var/run/vpn/pf.token"

// Rules is the ruleset that blocks everything leaving this Mac except what a
// reconnect and the local network need. It mirrors what the blackhole routes
// allow: loopback, the VPN server itself, private and link-local destinations,
// DHCP and multicast. "block return" rather than "drop", so applications fail
// at once instead of hanging until a timeout.
func Rules(serverIP string) (string, error) {
	ip := net.ParseIP(serverIP)
	if ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("kill switch needs the VPN server's IPv4 address, got %q", serverIP)
	}
	return strings.Join([]string{
		"pass out quick on lo0 all",
		"pass out quick inet proto udp from any to " + ip.To4().String(),
		"pass out quick inet proto udp from any port 68 to any port 67",
		"pass out quick inet from any to { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16, 224.0.0.0/4, 255.255.255.255 }",
		"pass out quick inet6 from any to { fe80::/10, ff02::/16 }",
		"block return out quick all",
	}, "\n") + "\n", nil
}

// Enable loads the rules and makes sure pf is running. Calling it again while
// it is already on just reloads the rules.
func Enable(serverIP string) error {
	rules, err := Rules(serverIP)
	if err != nil {
		return err
	}
	load := exec.Command(sysbin.Pfctl, "-a", Anchor, "-f", "-")
	load.Stdin = strings.NewReader(rules)
	if out, err := load.CombinedOutput(); err != nil {
		return fmt.Errorf("load kill-switch rules: %w (%s)", err, lastLine(out))
	}
	if _, err := os.Stat(tokenFile); err == nil {
		return nil // already holding a reference from an earlier Enable
	}
	out, err := exec.Command(sysbin.Pfctl, "-E").CombinedOutput()
	if err != nil {
		return fmt.Errorf("enable pf: %w (%s)", err, lastLine(out))
	}
	token := parseToken(out)
	if token == "" {
		return fmt.Errorf("enable pf: no token in pfctl output (%s)", lastLine(out))
	}
	if err := os.MkdirAll(filepath.Dir(tokenFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(tokenFile, []byte(token+"\n"), 0o600)
}

// Disable removes the rules and releases this client's reference on pf. It is
// safe to call when Enable never ran (after a crash, from `repair`): flushing
// an empty anchor and a missing token are both fine.
func Disable() error {
	var firstErr error
	if out, err := exec.Command(sysbin.Pfctl, "-a", Anchor, "-F", "all").CombinedOutput(); err != nil {
		firstErr = fmt.Errorf("flush kill-switch rules: %w (%s)", err, lastLine(out))
	}
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		// Enable never got as far as holding pf: there was nothing to remove,
		// and pfctl complaining about an anchor that does not exist is not a failure.
		return nil
	}
	_ = os.Remove(tokenFile)
	if token := parseToken(append([]byte("Token : "), data...)); token != "" {
		// A stale token (pf was reset since, or the Mac rebooted) is refused; that is fine.
		_ = exec.Command(sysbin.Pfctl, "-X", token).Run()
	}
	return firstErr
}

var tokenRE = regexp.MustCompile(`Token\s*:\s*(\d+)`)

// parseToken extracts the reference number from `pfctl -E` output
// ("pf enabled\nToken : 1234567890").
func parseToken(out []byte) string {
	if m := tokenRE.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}

func lastLine(out []byte) string {
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	return string(lines[len(lines)-1])
}
