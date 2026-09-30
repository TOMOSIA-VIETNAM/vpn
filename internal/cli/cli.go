// Package cli implements vpn's subcommands.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"vpn/internal/config"
	"vpn/internal/diagnostics"
	"vpn/internal/engine"
	"vpn/internal/keychain"
	"vpn/internal/privilege"
	"vpn/internal/secretinput"
)

// Version is set at build time via -ldflags "-X .../cli.Version=...".
var Version = "dev"

// Run dispatches argv[1:] to the matching subcommand. It returns the process
// exit code rather than calling os.Exit itself, so main stays a one-liner.
func Run(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 2
	}

	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "version":
		fmt.Println("vpn " + Version)
	case "profile":
		err = cmdProfile(rest)
	case "account":
		err = cmdAccount(rest)
	case "mtu":
		err = cmdMTU(rest)
	case "verbose":
		err = cmdVerbose(rest)
	case "killswitch":
		err = cmdKillSwitch(rest)
	case "strict":
		err = cmdStrict(rest)
	case "diagnose":
		err = cmdDiagnose(rest)
	case "connect":
		err = cmdConnect(rest)
	case "disconnect":
		err = cmdDisconnect(rest)
	case "status":
		err = cmdStatus(rest)
	case "repair":
		err = cmdRepair(rest)
	case "logs":
		err = cmdLogs(rest)
	case "update":
		err = cmdUpdate(rest)
	case "uninstall":
		err = cmdUninstall(rest)
	case "-h", "--help", "help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		printUsage()
		return 2
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func printUsage() {
	fmt.Fprint(os.Stderr, `vpn — native macOS L2TP/IPsec VPN client (multi-profile, multi-account)

Usage:
  vpn profile add <name> --server <host> [--server-id id] [--mtu n] [--full-tunnel] [--psk key]
  vpn profile list                  (* = active profile / default account)
  vpn profile edit <name> [--server host] [--user account] [--full-tunnel=bool] [--set-psk]   change host / username / tunnel mode; stored secrets are kept unless --set-psk
  vpn profile remove <name>
  vpn profile rename <name> [display name]   change the label shown in the app (empty: back to <name>); the key <name> and its stored secrets are untouched
  vpn account add <profile> <account> [--default] [--password pw]
  vpn mtu [1280|1400]               show / set the tunnel MTU for all profiles (applies on the next connect)
  vpn verbose [on|off]              show / set detailed per-packet logging for every connection (default off; applies on the next connect)
  vpn killswitch [on|off]           block all non-local traffic while a full-tunnel VPN reconnects (default off; applies on the next connect)
  vpn diagnose [--profile name] [--server host] [--json]
  vpn strict [on|off]               refuse legacy algorithms (3DES, MD5, DH under 2048 bits) instead of offering them (default off; applies on the next connect)
  vpn connect [--profile name] [--account name] [--timeout 30s] [--verbose] [--force] [--rekey-after 2m]  (always runs in the background)
  vpn disconnect
  vpn status [--json]
  vpn repair
  vpn logs [-f]
  vpn update [--force]              install the latest signed release if newer (--force: reinstall/downgrade)
  vpn update --check                print the latest signed release version as JSON (used by the app)
  vpn update --download-app <path>  save the verified app zip of the latest release (used by the app)
  vpn uninstall [-y]                remove the CLI, log, state, all profiles/accounts (Keychain included); not the menu bar app
  vpn version

Profiles cannot be edited or removed while connected — run `+"`vpn disconnect`"+` first.
`)
}

// newFlagSet keeps malformed user input inside Run's normal error path.
// flag.ExitOnError would terminate the process from deep inside a subcommand,
// skipping the CLI's consistent `Error: ...` handling and making commands hard
// to call from another Go process.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// flagsFirst accepts both conventional flag-first syntax and the natural
// command examples shown in help: `profile add work --server vpn.example`.
// Go's flag package stops parsing at the first positional argument, so these
// two commands need a small normalization layer before fs.Parse.
func flagsFirst(args []string, valueFlags map[string]bool) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.SplitN(arg, "=", 2)[0]
		if valueFlags[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positionals...)
}

// --- profile ---

func cmdProfile(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: vpn profile <add|list|edit|remove|rename> ...")
	}
	switch args[0] {
	case "add":
		return cmdProfileAdd(args[1:])
	case "list":
		return cmdProfileList(args[1:])
	case "edit":
		return cmdProfileEdit(args[1:])
	case "remove":
		return cmdProfileRemove(args[1:])
	case "rename":
		return cmdProfileRename(args[1:])
	default:
		return fmt.Errorf("unknown `profile` subcommand %q", args[0])
	}
}

// cmdProfileRename sets a profile's display name. The profile key stays put:
// the PSK and account passwords are filed in Keychain under it.
func cmdProfileRename(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: vpn profile rename <name> [display name]")
	}
	key := args[0]
	return changeProfile(key, "rename", func() error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		p, ok := cfg.Profiles[key]
		if !ok {
			return fmt.Errorf("unknown VPN profile %q", key)
		}
		p.DisplayName = strings.TrimSpace(strings.Join(args[1:], " "))
		if p.DisplayName == key {
			p.DisplayName = ""
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("Profile %q is now shown as %q.\n", key, p.Label(key))
		return nil
	})
}

// errIfInUse refuses a change to a profile whose tunnel is up (or coming up):
// the running daemon was started from its server, account and secrets.
func errIfInUse(profile, verb string) error {
	if st, busy := engine.ProfileInUse(profile); busy {
		return fmt.Errorf("cannot %s profile %q while it is %s — run `vpn disconnect` first", verb, profile, strings.ToLower(string(st.Phase)))
	}
	return nil
}

// changeProfile runs fn — which loads the config, mutates it and saves — under
// the connect lock, after re-checking the profile is idle. Checking alone would
// leave a window: a `vpn connect` starting right after the check could read the
// server, account and secrets fn is still rewriting or deleting. A connect
// reads all of them before it releases the lock (see ClaimNewConnect), so
// holding the lock here closes that window. Prompts for secrets belong BEFORE
// this call: nobody should hold up connects while typing.
func changeProfile(profile, verb string, fn func() error) error {
	return engine.WithConnectLockIfAvailable(func() error {
		if err := errIfInUse(profile, verb); err != nil {
			return err
		}
		return fn()
	})
}

// cmdProfileEdit changes a profile's host, default-account username or tunnel
// mode without touching the stored shared secret or password: the password is
// moved to the new username inside Keychain instead of being asked for again.
func cmdProfileEdit(args []string) error {
	fs := newFlagSet("profile edit")
	server := fs.String("server", "", "new VPN server host or IP")
	user := fs.String("user", "", "new username for the default account (its password is kept)")
	fullTunnel := fs.Bool("full-tunnel", true, "route all traffic through the VPN")
	setPSK := fs.Bool("set-psk", false, "also replace the shared secret (read from stdin / prompted)")
	if err := fs.Parse(flagsFirst(args, map[string]bool{"--server": true, "--user": true})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: vpn profile edit <name> [--server host] [--user account] [--full-tunnel=bool] [--set-psk]")
	}
	name := fs.Arg(0)
	ftSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "full-tunnel" {
			ftSet = true
		}
	})
	*server, *user = strings.TrimSpace(*server), strings.TrimSpace(*user)
	if *server == "" && *user == "" && !ftSet && !*setPSK {
		return fmt.Errorf("nothing to change — pass --server, --user, --full-tunnel and/or --set-psk")
	}

	newPSK := ""
	if *setPSK {
		// Fail fast, before asking for a secret: an unknown name or a live tunnel
		// would otherwise only be reported after the user has typed it in.
		if cfg, err := config.Load(); err != nil {
			return err
		} else if _, ok := cfg.Profiles[name]; !ok {
			return fmt.Errorf("unknown profile %q", name)
		}
		if err := errIfInUse(name, "edit"); err != nil {
			return err
		}
		v, err := secretinput.Prompt("IPsec pre-shared key (PSK)")
		if err != nil {
			return err
		}
		newPSK = v
	}

	return changeProfile(name, "edit", func() error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		p, ok := cfg.Profiles[name]
		if !ok {
			return fmt.Errorf("unknown profile %q", name)
		}
		if *setPSK {
			if err := keychain.SetPSK(name, newPSK); err != nil {
				return err
			}
		}
		if *server != "" {
			p.Server = *server
		}
		if ftSet {
			p.FullTunnel = *fullTunnel
		}
		if *user != "" && *user != p.DefaultAccount {
			if err := renameDefaultAccount(name, p, *user); err != nil {
				return err
			}
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("Profile %q updated (server=%s, account=%s); stored secrets kept.\n", name, p.Server, p.DefaultAccount)
		return nil
	})
}

// renameDefaultAccount points the profile's default account at newUser and
// carries the stored password across. With no account yet there is nothing to
// carry, so the name is just recorded (connect then asks for `account add`).
// Config is only saved by the caller, after Keychain has the new entry.
func renameDefaultAccount(profile string, p *config.Profile, newUser string) error {
	old := p.DefaultAccount
	if _, exists := p.Accounts[newUser]; !exists && old != "" && keychain.HasPassword(profile, old) {
		pw, err := keychain.GetPassword(profile, old)
		if err != nil {
			return fmt.Errorf("read the stored password of %q: %w", old, err)
		}
		if err := keychain.SetPassword(profile, newUser, pw); err != nil {
			return err
		}
	}
	if p.Accounts == nil {
		p.Accounts = map[string]*config.Account{}
	}
	if _, exists := p.Accounts[newUser]; !exists {
		p.Accounts[newUser] = &config.Account{Username: newUser}
		if old != "" {
			// The old name was renamed, not kept alongside.
			delete(p.Accounts, old)
			_ = keychain.DeletePassword(profile, old)
		}
	}
	p.DefaultAccount = newUser
	return nil
}

func cmdProfileAdd(args []string) error {
	fs := newFlagSet("profile add")
	server := fs.String("server", "", "VPN server host or IP (required)")
	serverID := fs.String("server-id", "", "expected IKE remote ID")
	mtu := fs.Int("mtu", 0, "this profile's MTU (default: keep the current one, else 1280); the global `vpn mtu` setting wins")
	fullTunnel := fs.Bool("full-tunnel", true, "route all traffic through the VPN")
	psk := fs.String("psk", "", "IPsec pre-shared key (prompted if omitted)")
	if err := fs.Parse(flagsFirst(args, map[string]bool{"--server": true, "--server-id": true, "--mtu": true, "--psk": true})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: vpn profile add <name> --server <host>")
	}
	name := fs.Arg(0)
	if *server == "" {
		return fmt.Errorf("--server is required")
	}
	if err := errIfInUse(name, "change"); err != nil { // fail fast, before asking for a secret
		return err
	}
	if *psk == "" {
		v, err := secretinput.Prompt("IPsec pre-shared key (PSK)")
		if err != nil {
			return err
		}
		*psk = v
	}

	return changeProfile(name, "change", func() error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		updated := cfg.AddProfile(name, &config.Profile{
			Server:     *server,
			ServerID:   *serverID,
			MTU:        *mtu,
			FullTunnel: *fullTunnel,
		})
		if err := keychain.SetPSK(name, *psk); err != nil {
			return err
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		verb := "added"
		if updated {
			verb = "updated (accounts kept)"
		}
		fmt.Printf("Profile %q %s (server=%s).\n", name, verb, *server)
		return nil
	})
}

// cmdMTU shows or sets the one tunnel MTU shared by every profile.
func cmdMTU(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	switch len(args) {
	case 0:
		fmt.Println(cfg.EffectiveMTU(nil))
		return nil
	case 1:
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("usage: vpn mtu [1280|1400]")
		}
		if err := cfg.SetMTU(n); err != nil {
			return err
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("MTU set to %d for all profiles (applies on the next connect).\n", n)
		return nil
	}
	return fmt.Errorf("usage: vpn mtu [1280|1400]")
}

// cmdVerbose shows or sets whether connections write per-packet debug logs.
func cmdVerbose(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	usage := fmt.Errorf("usage: vpn verbose [on|off]")
	switch len(args) {
	case 0:
		if cfg.EffectiveVerbose() {
			fmt.Println("on")
		} else {
			fmt.Println("off")
		}
		return nil
	case 1:
		switch args[0] {
		case "on", "true", "1":
			cfg.SetVerbose(true)
		case "off", "false", "0":
			cfg.SetVerbose(false)
		default:
			return usage
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("Detailed logging %s (applies on the next connect).\n", args[0])
		return nil
	}
	return usage
}

// cmdKillSwitch shows or sets whether traffic is blocked while reconnecting.
func cmdKillSwitch(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	usage := fmt.Errorf("usage: vpn killswitch [on|off]")
	switch len(args) {
	case 0:
		if cfg.KillSwitch {
			fmt.Println("on")
		} else {
			fmt.Println("off")
		}
		return nil
	case 1:
		switch args[0] {
		case "on", "true", "1":
			cfg.KillSwitch = true
		case "off", "false", "0":
			cfg.KillSwitch = false
		default:
			return usage
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("Kill switch %s (applies on the next connect; only for full-tunnel profiles).\n", args[0])
		if cfg.KillSwitch {
			fmt.Println("While the VPN reconnects, internet traffic is blocked. If it ever gets stuck: `vpn disconnect` or `vpn repair`.")
		}
		return nil
	}
	return usage
}

// cmdStrict shows or sets whether legacy algorithms are withheld from the proposals.
func cmdStrict(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	usage := fmt.Errorf("usage: vpn strict [on|off]")
	switch len(args) {
	case 0:
		if cfg.StrictCrypto {
			fmt.Println("on")
		} else {
			fmt.Println("off")
		}
		return nil
	case 1:
		switch args[0] {
		case "on", "true", "1":
			cfg.StrictCrypto = true
		case "off", "false", "0":
			cfg.StrictCrypto = false
		default:
			return usage
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("Strict encryption %s (applies on the next connect).\n", args[0])
		if cfg.StrictCrypto {
			fmt.Println("Servers that only support 3DES, MD5 or DH groups under 2048 bits will be refused.")
		}
		return nil
	}
	return usage
}

// cmdProfileList prints every profile — the CLI otherwise had no way to see
// what `vpn connect` would use.
func cmdProfileList(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: vpn profile list")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	names := cfg.ProfileNames()
	if len(names) == 0 {
		fmt.Println("No profiles yet — add one in the TOMOSIA VPN menu bar app or with `vpn profile add`.")
		return nil
	}
	for _, n := range names {
		fmt.Print(formatProfile(n, cfg.Profiles[n], n == cfg.ActiveProfile))
	}
	return nil
}

// formatProfile renders one profile for `profile list`: a "*" marks the
// active profile and the default account.
func formatProfile(name string, p *config.Profile, active bool) string {
	marker := " "
	if active {
		marker = "*"
	}
	tunnel := "full tunnel"
	if !p.FullTunnel {
		tunnel = "split tunnel"
	}
	var b strings.Builder
	label := name
	if p.DisplayName != "" {
		label = fmt.Sprintf("%s (%s)", p.DisplayName, name)
	}
	fmt.Fprintf(&b, "%s %s  server=%s  %s\n", marker, label, p.Server, tunnel)
	accounts := make([]string, 0, len(p.Accounts))
	for a := range p.Accounts {
		accounts = append(accounts, a)
	}
	sort.Strings(accounts)
	if len(accounts) == 0 {
		b.WriteString("    (no account — run `vpn account add " + name + " <username> --default`)\n")
	}
	for _, a := range accounts {
		m := " "
		if a == p.DefaultAccount {
			m = "*"
		}
		fmt.Fprintf(&b, "  %s account %s\n", m, a)
	}
	return b.String()
}

func cmdProfileRemove(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: vpn profile remove <name>")
	}
	name := args[0]
	return changeProfile(name, "remove", func() error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		p, ok := cfg.Profiles[name]
		if !ok {
			return fmt.Errorf("unknown profile %q", name)
		}
		for acct := range p.Accounts {
			_ = keychain.DeletePassword(name, acct)
		}
		_ = keychain.DeletePSK(name)
		delete(cfg.Profiles, name)
		if cfg.ActiveProfile == name {
			// Fall back to another remaining profile (alphabetically first, so
			// the choice is stable/predictable) rather than leaving no default
			// — otherwise a bare `vpn connect` would start failing right after
			// removing whichever profile happened to be active.
			cfg.ActiveProfile = ""
			if names := cfg.ProfileNames(); len(names) > 0 {
				cfg.ActiveProfile = names[0]
			}
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("Profile %q removed.\n", name)
		if cfg.ActiveProfile != "" {
			fmt.Printf("Active profile is now %q.\n", cfg.ActiveProfile)
		}
		return nil
	})
}

// --- account ---

func cmdAccount(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: vpn account add <profile> <username> [--default]")
	}
	switch args[0] {
	case "add":
		return cmdAccountAdd(args[1:])
	default:
		return fmt.Errorf("unknown `account` subcommand %q", args[0])
	}
}

func cmdAccountAdd(args []string) error {
	fs := newFlagSet("account add")
	makeDefault := fs.Bool("default", false, "make this the profile's default account")
	password := fs.String("password", "", "account password (prompted if omitted)")
	if err := fs.Parse(flagsFirst(args, map[string]bool{"--password": true})); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: vpn account add <profile> <username> [--default]")
	}
	profileName, username := fs.Arg(0), fs.Arg(1)

	// Fail on an unknown profile before asking for a password.
	if cfg, err := config.Load(); err != nil {
		return err
	} else if _, _, err := cfg.Profile(profileName); err != nil {
		return err
	}
	if err := errIfInUse(profileName, "change"); err != nil { // fail fast, before asking for a secret
		return err
	}
	if *password == "" {
		v, err := secretinput.Prompt(fmt.Sprintf("Password for %s@%s", username, profileName))
		if err != nil {
			return err
		}
		*password = v
	}

	return changeProfile(profileName, "change", func() error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		_, p, err := cfg.Profile(profileName)
		if err != nil {
			return err
		}
		if p.Accounts == nil {
			p.Accounts = map[string]*config.Account{}
		}
		p.Accounts[username] = &config.Account{Username: username}
		if *makeDefault || p.DefaultAccount == "" {
			p.DefaultAccount = username
		}
		if err := keychain.SetPassword(profileName, username, *password); err != nil {
			return err
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("Account %q added to profile %q.\n", username, profileName)
		return nil
	})
}

// --- diagnose ---

func cmdDiagnose(args []string) error {
	fs := newFlagSet("diagnose")
	profileName := fs.String("profile", "", "profile to diagnose (default: active profile)")
	server := fs.String("server", "", "diagnose an arbitrary host instead of a saved profile")
	asJSON := fs.Bool("json", false, "output as JSON")
	timeout := fs.Duration("timeout", 20_000_000_000, "overall diagnostic timeout") // 20s
	if err := fs.Parse(args); err != nil {
		return err
	}

	target := *server
	if target == "" {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		_, p, err := cfg.Profile(*profileName)
		if err != nil {
			return err
		}
		target = p.Server
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report := diagnostics.Run(ctx, target, privilege.Elevate)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	printReport(report)
	return nil
}

func printReport(r *diagnostics.Report) {
	ok := func(b bool) string {
		if b {
			return "[OK]"
		}
		return "[FAIL]"
	}
	fmt.Printf("%s Network      interface=%s ip=%s gateway=%s mtu=%d\n", ok(r.Network.Interface != ""), r.Network.Interface, r.Network.IPv4, r.Network.Gateway, r.Network.MTU)
	fmt.Printf("%s DNS          servers=%v\n", ok(len(r.Network.DNSServers) > 0), r.Network.DNSServers)
	fmt.Printf("%s VPN DNS      server=%s resolved=%v\n", ok(r.Connectivity.DNSOK), r.Connectivity.ServerHost, r.Connectivity.ResolvedIPs)
	if r.Connectivity.PingRTTMillis > 0 {
		fmt.Printf("     ping avg RTT: %.1fms\n", r.Connectivity.PingRTTMillis)
	}
	fmt.Printf("%s UDP/500      (IKE responder answered)\n", ok(r.Connectivity.UDP500Reached))
	switch r.Connectivity.FromPort500 {
	case "ok":
		fmt.Println("[OK] UDP/500      from local port 500")
	case "no-answer":
		fmt.Println("[WARN] UDP/500    no answer from local port 500 — this network drops IKE sourced from 500; `vpn connect` falls back to another port automatically")
	default:
		fmt.Println("[SKIP] UDP/500    from local port 500 (needs root, or the port is busy)")
	}
	fmt.Printf("%s UDP/4500     (NAT-T responder answered)\n", ok(r.Connectivity.UDP4500Reached))
	fmt.Println("MTU probes:")
	for _, p := range r.MTUProbes {
		fmt.Printf("  %s %d bytes\n", ok(p.OK), p.Size)
	}
	if r.FailureStage != "" {
		fmt.Printf("\nFAIL\nReason:\n  %s\n", r.FailureStage)
	} else {
		fmt.Println("\nAll pre-flight checks passed — ready to attempt `vpn connect`.")
	}
	if len(r.Errors) > 0 {
		fmt.Println("Errors:")
		for _, e := range r.Errors {
			fmt.Println("  " + e)
		}
	}
}
