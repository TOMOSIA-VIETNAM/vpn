package config

import (
	"encoding/json"
	"testing"
)

func TestAddProfileUpdateKeepsAccounts(t *testing.T) {
	c := &Config{}
	if c.AddProfile("work", &Profile{Server: "old.example", FullTunnel: true}) {
		t.Fatal("first add reported as update")
	}
	p := c.Profiles["work"]
	p.Accounts["alice"] = &Account{Username: "alice"}
	p.DefaultAccount = "alice"
	p.ESPProposals = []string{"aes128-sha1"} // hand-edited

	if !c.AddProfile("work", &Profile{Server: "new.example", FullTunnel: false}) {
		t.Fatal("re-add not reported as update")
	}
	got := c.Profiles["work"]
	if got.Server != "new.example" || got.FullTunnel {
		t.Fatalf("new settings not applied: %+v", got)
	}
	if _, ok := got.Accounts["alice"]; !ok || got.DefaultAccount != "alice" {
		t.Fatalf("re-add dropped accounts: %+v", got)
	}
	if len(got.ESPProposals) != 1 || got.ESPProposals[0] != "aes128-sha1" {
		t.Fatalf("re-add reset hand-edited proposals: %v", got.ESPProposals)
	}
}

func TestProfileNamesSorted(t *testing.T) {
	c := &Config{}
	for _, n := range []string{"b", "c", "a"} {
		c.AddProfile(n, &Profile{Server: n})
	}
	if got := c.ProfileNames(); len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("ProfileNames = %v", got)
	}
	if c.ActiveProfile != "b" {
		t.Fatalf("active profile = %q, want the first one added", c.ActiveProfile)
	}
}

func TestGlobalMTUWinsOverProfile(t *testing.T) {
	c := &Config{}
	c.AddProfile("a", &Profile{Server: "a", MTU: 1400})
	c.AddProfile("b", &Profile{Server: "b"})
	if got := c.EffectiveMTU(c.Profiles["a"]); got != 1400 {
		t.Fatalf("no global setting: mtu = %d, want the profile's 1400", got)
	}
	if err := c.SetMTU(1280); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a", "b"} {
		if got := c.EffectiveMTU(c.Profiles[n]); got != 1280 {
			t.Fatalf("profile %s: mtu = %d, want the global 1280", n, got)
		}
	}
}

func TestSetMTURejectsOtherValues(t *testing.T) {
	c := &Config{}
	for _, n := range []int{0, 1200, 1500, -1} {
		if err := c.SetMTU(n); err == nil {
			t.Fatalf("SetMTU(%d) accepted", n)
		}
	}
	if c.MTU != 0 {
		t.Fatalf("rejected value was stored: %d", c.MTU)
	}
}

func TestReAddKeepsProfileMTU(t *testing.T) {
	c := &Config{}
	c.AddProfile("w", &Profile{Server: "s", MTU: 1280})
	c.AddProfile("w", &Profile{Server: "s2"}) // e.g. the UI's edit form, which passes no --mtu
	if got := c.Profiles["w"].MTU; got != 1280 {
		t.Fatalf("re-add reset MTU to %d, want 1280 kept", got)
	}
}

func TestVerboseDefaultsOffAndPersists(t *testing.T) {
	c := &Config{Profiles: map[string]*Profile{}}
	if c.EffectiveVerbose() {
		t.Fatal("detailed logging must be off unless the user turned it on")
	}
	c.SetVerbose(true)
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Config
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !back.EffectiveVerbose() {
		t.Fatal("an explicit on did not survive a save/load round trip")
	}
}

func TestKillSwitchDefaultsOffAndPersists(t *testing.T) {
	var c Config
	if c.KillSwitch {
		t.Fatal("the kill switch blocks the network, so it must be opt-in")
	}
	c.KillSwitch = true
	data, _ := json.Marshal(&c)
	var back Config
	if err := json.Unmarshal(data, &back); err != nil || !back.KillSwitch {
		t.Fatalf("kill switch lost in a save/load round trip: %v %+v", err, back)
	}
}

func TestDisplayNameLabelAndSurvivesReAdd(t *testing.T) {
	p := &Profile{Server: "s"}
	if got := p.Label("Hinode"); got != "Hinode" {
		t.Fatalf("no display name: label %q, want the key", got)
	}
	c := &Config{Profiles: map[string]*Profile{}}
	c.AddProfile("Hinode", &Profile{Server: "s", DisplayName: "Office VPN"})
	if got := c.Profiles["Hinode"].Label("Hinode"); got != "Office VPN" {
		t.Fatalf("label %q, want the display name", got)
	}
	// Editing the server later (profile add again) must not wipe the display name.
	c.AddProfile("Hinode", &Profile{Server: "s2"})
	if got := c.Profiles["Hinode"].DisplayName; got != "Office VPN" {
		t.Fatalf("re-adding the profile lost the display name (%q)", got)
	}
}

func TestStrongProposalsDropsLegacyAlgorithms(t *testing.T) {
	in := []string{"aes256-sha256-modp2048", "3des-sha1-modp2048", "aes256-md5-modp1024", "aes128-sha1-modp1024", "aes256-sha1-modp1536", "aes256-sha256", "3des-md5", "aes128-sha256-modp3072"}
	got := StrongProposals(in)
	want := []string{"aes256-sha256-modp2048", "aes256-sha256", "aes128-sha256-modp3072"}
	if len(got) != len(want) {
		t.Fatalf("StrongProposals = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("StrongProposals = %v, want %v", got, want)
		}
	}
	if len(StrongProposals(DefaultIKEProposals)) == 0 || len(StrongProposals(DefaultESPProposals)) == 0 {
		t.Fatal("the default proposals must leave something once legacy algorithms are removed")
	}
}
