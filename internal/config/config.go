// Package config manages vpn's non-secret profile/account configuration.
// Secrets (PSK, account passwords) are never stored here — see internal/keychain.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Account is one login identity that can be used against a Profile's server.
// A single VPN server commonly has several accounts (e.g. issued to different
// people, or a spare when the primary is already logged in elsewhere), so a
// Profile keeps a map of these rather than a single username/password pair.
type Account struct {
	Username string `json:"username"`
}

// Profile is one VPN server configuration, independent of which account is
// currently used to connect to it.
type Profile struct {
	// DisplayName is the label shown to the user; empty means the profile's
	// key. The key itself never changes: Keychain entries (PSK, passwords) are
	// filed under it, so renaming the key would orphan every stored secret.
	DisplayName    string              `json:"display_name,omitempty"`
	Server         string              `json:"server"`
	ServerID       string              `json:"server_id,omitempty"`
	IKEProposals   []string            `json:"ike_proposals,omitempty"`
	ESPProposals   []string            `json:"esp_proposals,omitempty"`
	MTU            int                 `json:"mtu,omitempty"`
	FullTunnel     bool                `json:"full_tunnel"`
	DefaultAccount string              `json:"default_account,omitempty"`
	Accounts       map[string]*Account `json:"accounts"`
}

// Config is the on-disk, non-secret configuration for every known VPN
// profile plus which one is currently selected.
type Config struct {
	// MTU, when set, is the tunnel MTU for every profile and wins over the
	// per-profile value: it is a property of the network the user is on, not
	// of a server, so it is one switch rather than one per profile.
	MTU int `json:"mtu,omitempty"`
	// Verbose, when set, turns per-packet debug logging on or off for every
	// connection. Nil means the default (off — see EffectiveVerbose).
	Verbose *bool `json:"verbose,omitempty"`
	// KillSwitch, when on, blocks all non-local traffic while a full-tunnel
	// VPN reconnects instead of letting it flow around the tunnel unprotected.
	KillSwitch    bool                `json:"kill_switch,omitempty"`
	ActiveProfile string              `json:"active_profile,omitempty"`
	Profiles      map[string]*Profile `json:"profiles"`

	path string // resolved on Load/New, not persisted
}

// DefaultMTU is used when neither the global setting nor the profile names one.
const DefaultMTU = 1280

// AllowedMTUs are the values a user may pick: 1400 fits ordinary links,
// 1280 survives PPPoE, hotspots and other paths that fragment or drop the
// ESP-encapsulated larger packets.
var AllowedMTUs = []int{1280, 1400}

// ValidMTU reports whether n is one of AllowedMTUs.
func ValidMTU(n int) bool {
	for _, v := range AllowedMTUs {
		if v == n {
			return true
		}
	}
	return false
}

// SetMTU sets the global tunnel MTU for all profiles.
func (c *Config) SetMTU(n int) error {
	if !ValidMTU(n) {
		return fmt.Errorf("mtu must be 1280 or 1400, got %d", n)
	}
	c.MTU = n
	return nil
}

// EffectiveMTU is the MTU a connection using p should run with: the global
// setting, else the profile's own, else DefaultMTU.
func (c *Config) EffectiveMTU(p *Profile) int {
	if c.MTU != 0 {
		return c.MTU
	}
	if p != nil && p.MTU != 0 {
		return p.MTU
	}
	return DefaultMTU
}

// EffectiveVerbose reports whether connections should write protocol-level debug
// logging. Off unless the user turned it on. Connection milestones, errors and
// the periodic "tunnel alive" line are logged regardless (see internal/vpnlog),
// so a drop is diagnosable without it; this only adds handshake and
// retransmit detail.
func (c *Config) EffectiveVerbose() bool {
	return c.Verbose != nil && *c.Verbose
}

// SetVerbose sets the global verbose-logging switch.
func (c *Config) SetVerbose(on bool) { c.Verbose = &on }

// DefaultIKEProposals offers a comprehensive list of IKE Phase 1 proposals
// in preference order (strongest/most modern first). This matches macOS native
// and Windows VPN client behavior to ensure compatibility with diverse VPN servers
// (Cisco, Fortinet, pfSense, Windows Server RRAS, MikroTik, strongSwan/Libreswan).
var DefaultIKEProposals = []string{
	"aes256-sha512-modp4096",
	"aes256-sha384-modp3072",
	"aes256-sha256-modp4096",
	"aes256-sha256-modp3072",
	"aes256-sha256-modp2048",
	"aes256-sha256-modp1536",
	"aes256-sha256-modp1024",
	"aes128-sha256-modp2048",
	"aes128-sha256-modp1536",
	"aes128-sha256-modp1024",
	"aes256-sha1-modp2048",
	"aes256-sha1-modp1536",
	"aes256-sha1-modp1024",
	"aes128-sha1-modp2048",
	"aes128-sha1-modp1536",
	"aes128-sha1-modp1024",
	"3des-sha1-modp2048",
	"3des-sha1-modp1024",
	"aes256-md5-modp1024",
	"aes128-md5-modp1024",
	"3des-md5-modp1024",
}

// DefaultESPProposals offers a comprehensive list of ESP (Phase 2) transforms
// in preference order, ensuring compatibility with all L2TP/IPsec VPN servers.
var DefaultESPProposals = []string{
	"aes256-sha512",
	"aes256-sha384",
	"aes256-sha256",
	"aes256-sha1",
	"aes128-sha256",
	"aes128-sha1",
	"3des-sha1",
	"aes256-md5",
	"aes128-md5",
	"3des-md5",
}

// Dir returns ~/.config/vpn, creating it with 0700 permissions if missing.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	dir := filepath.Join(home, ".config", "vpn")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}
	return dir, nil
}

func filePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the config file, returning an empty Config if it does not exist
// yet (first run — nothing has been added via `vpn profile add`).
func Load() (*Config, error) {
	path, err := filePath()
	if err != nil {
		return nil, err
	}
	cfg := &Config{Profiles: map[string]*Profile{}, path: path}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.path = path
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*Profile{}
	}
	return cfg, nil
}

// Save writes the config atomically with 0600 permissions — it may name a
// server address but never a secret.
func (c *Config) Save() error {
	if c.path == "" {
		p, err := filePath()
		if err != nil {
			return err
		}
		c.path = p
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return fmt.Errorf("commit config: %w", err)
	}
	return nil
}

// Profile looks up a profile by name, or the active one when name is "".
func (c *Config) Profile(name string) (string, *Profile, error) {
	if name == "" {
		name = c.ActiveProfile
	}
	if name == "" {
		return "", nil, fmt.Errorf("no VPN profile selected — add one in the TMS VPN menu bar app or pass --profile")
	}
	p, ok := c.Profiles[name]
	if !ok {
		return "", nil, fmt.Errorf("unknown VPN profile %q", name)
	}
	return name, p, nil
}

// Label is what to call this profile in the UI: its display name, else its key.
func (p *Profile) Label(key string) string {
	if p != nil && p.DisplayName != "" {
		return p.DisplayName
	}
	return key
}

// Account looks up an account on a profile by name, or the profile's default
// when name is "".
func (p *Profile) Account(name string) (string, *Account, error) {
	if name == "" {
		name = p.DefaultAccount
	}
	if name == "" {
		return "", nil, fmt.Errorf("no account selected for this profile")
	}
	a, ok := p.Accounts[name]
	if !ok {
		return "", nil, fmt.Errorf("unknown account %q", name)
	}
	return name, a, nil
}

// AddProfile creates or replaces a profile definition. It fills in the
// compatibility-reference IKE/ESP proposals and MTU when the caller leaves
// them unset, rather than inventing different defaults per call site.
//
// Re-adding an existing name updates it in place: the accounts, default
// account and any hand-edited proposals of the existing profile carry over
// unless p sets them. Replacing it wholesale used to drop every account
// from the config while their passwords stayed orphaned in Keychain.
// Reports whether the profile already existed.
func (c *Config) AddProfile(name string, p *Profile) (updated bool) {
	if old, ok := c.Profiles[name]; ok {
		updated = true
		if len(p.Accounts) == 0 {
			p.Accounts = old.Accounts
		}
		if p.DefaultAccount == "" {
			p.DefaultAccount = old.DefaultAccount
		}
		if len(p.IKEProposals) == 0 {
			p.IKEProposals = old.IKEProposals
		}
		if len(p.ESPProposals) == 0 {
			p.ESPProposals = old.ESPProposals
		}
		if p.MTU == 0 {
			p.MTU = old.MTU
		}
		if p.DisplayName == "" {
			p.DisplayName = old.DisplayName
		}
	}
	if len(p.IKEProposals) == 0 {
		p.IKEProposals = append([]string(nil), DefaultIKEProposals...)
	}
	if len(p.ESPProposals) == 0 {
		p.ESPProposals = append([]string(nil), DefaultESPProposals...)
	}
	if p.MTU == 0 {
		p.MTU = DefaultMTU
	}
	if p.Accounts == nil {
		p.Accounts = map[string]*Account{}
	}
	if c.Profiles == nil {
		c.Profiles = map[string]*Profile{}
	}
	c.Profiles[name] = p
	if c.ActiveProfile == "" {
		c.ActiveProfile = name
	}
	return updated
}

// ProfileNames returns every profile name, sorted.
func (c *Config) ProfileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
