package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"vpn/internal/config"
	"vpn/internal/ike"
	"vpn/internal/vpnlog"
)

// The LNS-side tunnel/session IDs of the live session are kept on disk, so a
// session the client could not close — killed daemon, crash, reboot, power
// loss — can still be ended by the next connect (see evictStale) instead of
// leaving the account "already logged in" until the server's pppd gives up on
// its echoes (~75 s).
//
// It lives in the user's config directory, not /var/run/vpn: that one is
// cleared at boot, which is exactly when this is needed most.

// lastSessionMaxAge bounds how long after the client was last seen alive a
// record is still acted on. The server drops an abandoned session within a
// couple of minutes anyway; past that a Terminate has nothing to end, and the
// older the record the likelier the server has reused those IDs elsewhere.
const lastSessionMaxAge = 30 * time.Minute

// lastSessionRefresh is how often the liveness timestamp is rewritten while
// connected — one tiny write every few minutes.
const lastSessionRefresh = 5 * time.Minute

type lastSession struct {
	Server      string    `json:"server"`
	PeerTunnel  uint16    `json:"peer_tunnel"`
	PeerSession uint16    `json:"peer_session"`
	L2TPPort    uint16    `json:"l2tp_port,omitempty"` // absent in records from before per-connection ports: 1701
	AliveAt     time.Time `json:"alive_at"`
}

func lastSessionPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

// saveLastSession records the session just established (or refreshes its
// liveness stamp). Best effort: failing to save only loses the eviction.
func saveLastSession(server string, tunnel, session, l2tpPort uint16) {
	p, err := lastSessionPath()
	if err != nil {
		return
	}
	data, err := json.Marshal(lastSession{Server: server, PeerTunnel: tunnel, PeerSession: session, L2TPPort: l2tpPort, AliveAt: time.Now()})
	if err != nil {
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		vpnlog.Error("ENGINE", "could not record the session IDs", vpnlog.Fields{"err": err})
		return
	}
	_ = os.Rename(tmp, p)
}

// clearLastSession removes the record once the session was closed properly.
func clearLastSession() {
	if p, err := lastSessionPath(); err == nil {
		_ = os.Remove(p)
	}
}

// loadLastSession returns the session left behind by an earlier run against
// this server, or nil if there is none worth acting on.
func loadLastSession(server string, now time.Time) *staleSession {
	p, err := lastSessionPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var ls lastSession
	if json.Unmarshal(data, &ls) != nil || ls.Server != server || ls.PeerTunnel == 0 || ls.PeerSession == 0 {
		return nil
	}
	if age := now.Sub(ls.AliveAt); age < 0 || age > lastSessionMaxAge {
		return nil
	}
	port := ls.L2TPPort
	if port == 0 {
		port = ike.L2TPPort
	}
	return &staleSession{tunnel: ls.PeerTunnel, session: ls.PeerSession, l2tpPort: port}
}
