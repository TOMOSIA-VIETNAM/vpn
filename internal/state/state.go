// Package state persists the running tunnel's status to disk so `status`,
// `disconnect`, and `repair` can inspect or act on a connection started by a
// different process invocation (connect runs in the foreground/daemonized;
// these commands are separate CLI invocations).
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Phase mirrors the engine's coarse connection lifecycle, independent of the
// fine-grained IKE/PPP sub-state-machines.
type Phase string

const (
	PhaseDisconnected Phase = "DISCONNECTED"
	PhaseConnecting   Phase = "CONNECTING"
	PhaseConnected    Phase = "CONNECTED"
	PhaseFailed       Phase = "FAILED"
)

// State is the on-disk snapshot of the running (or last) connection.
type State struct {
	Phase       Phase     `json:"phase"`
	Profile     string    `json:"profile,omitempty"`
	Account     string    `json:"account,omitempty"`
	Server      string    `json:"server,omitempty"`
	PID         int       `json:"pid,omitempty"`
	TunDevice   string    `json:"tun_device,omitempty"`
	LocalIP     string    `json:"local_ip,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
	FailStage   string    `json:"fail_stage,omitempty"`
	FailDetail  string    `json:"fail_detail,omitempty"`
	SavedRoutes bool      `json:"saved_routes"` // true once original routing/DNS captured for repair/restore

	// Reconnecting is set (with Phase CONNECTING) while the daemon is
	// re-establishing a tunnel it lost, on its own — the UI must not treat it
	// as a failure or start a competing connect. Reconnects counts how many
	// times this daemon has had to do so; FailDetail then holds the reason
	// for the most recent drop or failed attempt.
	Reconnecting bool `json:"reconnecting,omitempty"`
	Reconnects   int  `json:"reconnects,omitempty"`

	// DNS snapshot, captured before Apply so disconnect/repair can restore
	// it even if that's a different process invocation than the one that
	// connected (e.g. after a crash — see dnsmgr.Snapshot). DNSApplied
	// distinguishes "no DNS servers were pushed" (nothing to restore) from
	// "the original config was itself empty/DHCP" (restore to Empty).
	DNSService string   `json:"dns_service,omitempty"`
	DNSServers []string `json:"dns_servers,omitempty"`
	DNSApplied bool     `json:"dns_applied,omitempty"`

	// Warnings are privacy caveats about the live connection (e.g. the LNS
	// pushed no DNS servers) that connect and status surface to the user.
	Warnings []string `json:"warnings,omitempty"`
}

// Dir is where the state file lives — exported so `uninstall` can remove it
// without needing its own copy of the path.
const Dir = "/var/run/vpn"

// path is where the state file lives, without creating anything — used by
// Load, which must work read-only and unprivileged (e.g. plain `vpn
// status`, before this user has ever connected and root has never had a
// reason to create Dir yet).
func path() string {
	return filepath.Join(Dir, "state.json")
}

// Load reads the current state, returning a DISCONNECTED state if no file
// (or not even Dir itself) exists yet — nothing has ever connected, and
// this must not try to create Dir itself: Dir lives under /var/run, so
// only Save (always called while privilege.Elevate has this process at
// root — see connect/disconnect/repair/uninstall) is allowed to create it.
func Load() (*State, error) {
	data, err := os.ReadFile(path())
	if os.IsNotExist(err) {
		return &State{Phase: PhaseDisconnected}, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save persists s atomically. Callers must already be root (see
// privilege.Elevate) — /var/run/vpn is root-owned, and this is what
// creates it on first use.
func (s *State) Save() error {
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return err
	}
	_ = os.Chmod(Dir, 0o755)
	p := path()
	s.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	// World-readable on purpose: the menu bar app polls this file directly
	// as the unprivileged user. It holds no secrets — profile/account
	// names, server host, tunnel address and DNS snapshot only.
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	_ = os.Chmod(tmp, 0o644)
	return os.Rename(tmp, p)
}

// Clear resets to DISCONNECTED, used once disconnect/repair has actually
// torn everything down.
func Clear() error {
	s := &State{Phase: PhaseDisconnected}
	return s.Save()
}
