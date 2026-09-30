package pffw

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vpn/internal/sysbin"
)

func TestRulesPassOnlyLocalAndServerThenBlock(t *testing.T) {
	rules, err := Rules("203.0.113.7")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(rules), "\n")
	if last := lines[len(lines)-1]; last != "block return out quick all" {
		t.Fatalf("the ruleset must end by blocking everything else, got %q", last)
	}
	for _, l := range lines[:len(lines)-1] {
		if !strings.HasPrefix(l, "pass out quick ") {
			t.Fatalf("unexpected rule before the final block: %q", l)
		}
	}
	if !strings.Contains(rules, "proto udp from any to 203.0.113.7\n") {
		t.Fatal("the VPN server must stay reachable, or the VPN could never reconnect")
	}
	// Nothing may pass to the internet at large.
	if n := strings.Count(rules, "to any"); n != 1 { // only DHCP (port 68 -> 67)
		t.Fatalf("%d rules pass to any destination, want only the DHCP one", n)
	}
}

func TestRulesRejectWhatIsNotAnIPv4Address(t *testing.T) {
	for _, bad := range []string{"", "vpn.example.com", "2001:db8::1", "1.2.3.4; pass out all", "1.2.3.4\npass out all"} {
		if r, err := Rules(bad); err == nil {
			t.Fatalf("Rules(%q) = %q, want an error", bad, r)
		}
	}
}

// pfctl -n parses a ruleset without loading it and needs no privileges, so the
// exact text Enable would load is checked against the real parser.
func TestRulesParseWithPfctl(t *testing.T) {
	if _, err := os.Stat(sysbin.Pfctl); err != nil {
		t.Skip("no pfctl on this system")
	}
	rules, err := Rules("203.0.113.7")
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "rules.pf")
	if err := os.WriteFile(f, []byte(rules), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(sysbin.Pfctl, "-n", "-f", f).CombinedOutput(); err != nil {
		t.Fatalf("pfctl rejects the kill-switch rules: %v\n%s", err, out)
	}
	// ...and the check really does catch a broken ruleset.
	if err := os.WriteFile(f, []byte("pass out quick frm any\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(sysbin.Pfctl, "-n", "-f", f).Run(); err == nil {
		t.Fatal("pfctl -n accepted a malformed rule: this test proves nothing")
	}
}

func TestParseToken(t *testing.T) {
	if got := parseToken([]byte("No ALTQ support in kernel\npf enabled\nToken : 12345678901234567890\n")); got != "12345678901234567890" {
		t.Fatalf("parseToken = %q", got)
	}
	if got := parseToken([]byte("pfctl: Operation not permitted")); got != "" {
		t.Fatalf("parseToken of an error = %q", got)
	}
}
