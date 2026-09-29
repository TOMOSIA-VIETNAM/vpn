package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"vpn/internal/config"
	"vpn/internal/engine"
	"vpn/internal/keychain"
	"vpn/internal/privilege"
	"vpn/internal/state"
	"vpn/internal/sysbin"
)

// daemonChildEnv marks a re-exec'd `connect` invocation as the detached
// background process itself, so it runs the real connect logic in place
// instead of re-spawning another daemon (which would otherwise recurse
// forever) — see cmdConnect.
const daemonChildEnv = "VPN_DAEMON_CHILD"

func cmdConnect(args []string) error {
	fs := newFlagSet("connect")
	profileName := fs.String("profile", "", "profile to connect (default: active profile)")
	accountName := fs.String("account", "", "account to use (default: profile's default account)")
	timeout := fs.Duration("timeout", 30*time.Second, "overall connect timeout")
	verbose := fs.Bool("verbose", false, "verbose protocol logging")
	force := fs.Bool("force", false, "reconnect even if this profile/account is already connected")
	rekeyAfter := fs.Duration("rekey-after", 0, "rekey the ESP SAs this often instead of at half their lifetime (testing)")
	// -d/--daemon are accepted but always on — connect always backgrounds
	// itself now; the flags exist only so old scripts/muscle memory using
	// `vpn connect -d` don't break.
	fs.Bool("d", true, "run in background (default; kept for compatibility)")
	fs.Bool("daemon", true, "alias of -d")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if os.Getenv(daemonChildEnv) == "1" {
		return doConnect(*profileName, *accountName, *timeout, *verbose, *rekeyAfter)
	}

	// Catch configuration problems here, where the user can see them: the
	// daemon's stderr is /dev/null, so if it exited over a missing account
	// or secret, all this process could report is that it never started.
	t, err := resolveTarget(*profileName, *accountName)
	if err != nil {
		return err
	}
	if err := t.checkSecretsStored(); err != nil {
		return err
	}

	// Tear down any previous connect/connected session, fork the new
	// daemon, and wait for it to record its own PID — all under one lock,
	// so a `disconnect` invoked right after this call is guaranteed to see
	// either the old session or the new one, never neither. See
	// engine.PrepareNewConnect/ClaimNewConnect's doc comments for the race
	// this closes; it's why this can't just be two separate steps like it
	// used to be.
	var cmd *exec.Cmd
	alreadyUp := false
	err = engine.WithConnectLock(func() error {
		// Already serving this very profile/account (connected, or
		// reconnecting by itself): leave it running. Replacing a healthy
		// session would drop the user's traffic and the server-side login.
		if !*force {
			if st, ok := engine.AlreadyServing(t.profileName, t.accountName); ok {
				alreadyUp = true
				fmt.Printf("Already %s (pid %d) — nothing to do. Use `vpn connect --force` to reconnect anyway.\n", strings.ToLower(string(st.Phase)), st.PID)
				return nil
			}
		}
		if err := engine.PrepareNewConnect(); err != nil {
			return fmt.Errorf("disconnect previous session: %w", err)
		}
		var err error
		cmd, err = startDaemon(args)
		if err != nil {
			return err
		}
		return engine.ClaimNewConnect(cmd.Process.Pid, 2*time.Second)
	})
	if err != nil {
		return err
	}
	if alreadyUp {
		return nil
	}

	fmt.Printf("Connecting in the background (pid %d)...\n", cmd.Process.Pid)
	return awaitOutcome(*timeout)
}

// startDaemon re-execs this same binary as a detached background process
// (new session via Setsid, stdio pointed at /dev/null so it survives the
// parent's terminal closing) running the real connect logic.
func startDaemon(args []string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate own executable to re-exec as a daemon: %w", err)
	}

	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	defer devnull.Close()

	cmd := exec.Command(exe, append([]string{"connect"}, args...)...)
	cmd.Env = append(os.Environ(), daemonChildEnv+"=1")
	cmd.Stdin = devnull
	cmd.Stdout = devnull
	cmd.Stderr = devnull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach from this terminal's session
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start background connect: %w", err)
	}
	return cmd, nil
}

// awaitOutcome polls state until the just-started daemon reaches
// CONNECTED/FAILED (or timeout) so the caller gets immediate feedback
// instead of a background process starting silently with no way to tell
// whether it actually worked. Deliberately outside WithConnectLock — a
// negotiation can take the full timeout, and a disconnect issued while
// it's in flight must be able to interrupt it promptly, not wait for it.
func awaitOutcome(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(30 * time.Millisecond)
		st, err := state.Load()
		if err != nil {
			continue
		}
		switch st.Phase {
		case state.PhaseConnected:
			fmt.Printf("Connected (local IP %s, device %s).\n", st.LocalIP, st.TunDevice)
			printWarnings(st)
			return nil
		case state.PhaseFailed:
			return fmt.Errorf("%s: %s", st.FailStage, st.FailDetail)
		}
	}
	fmt.Println("Still connecting in the background — check `vpn status` or `vpn logs -f`.")
	return nil
}

// doConnect is the actual connect logic, run inside the detached daemon
// process spawned by spawnDaemon (see daemonChildEnv).
// connectTarget is the profile/account pair a connect resolves to.
type connectTarget struct {
	profileName string
	profile     *config.Profile
	accountName string
	mtu         int  // config.EffectiveMTU: the global setting, else the profile's
	verbose     bool // config.EffectiveVerbose: the global logging setting
	killSwitch  bool // config.KillSwitch: block traffic while reconnecting
}

// resolveTarget resolves the profile and account a connect would use —
// shared by the foreground preflight in cmdConnect and doConnect itself.
func resolveTarget(profileName, accountName string) (*connectTarget, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	pName, p, err := cfg.Profile(profileName)
	if err != nil {
		return nil, err
	}
	aName, _, err := p.Account(accountName)
	if err != nil {
		return nil, fmt.Errorf("profile %q: %w — run `vpn account add %s <username> --default`", pName, err, pName)
	}
	return &connectTarget{profileName: pName, profile: p, accountName: aName, mtu: cfg.EffectiveMTU(p), verbose: cfg.EffectiveVerbose(), killSwitch: cfg.KillSwitch}, nil
}

// checkSecretsStored confirms the PSK and password exist in Keychain
// without reading them — cmdConnect's preflight; the daemon simply reads
// them and reports the same errors if they are missing.
func (t *connectTarget) checkSecretsStored() error {
	if !keychain.HasPSK(t.profileName) {
		return t.errNoPSK(nil)
	}
	if !keychain.HasPassword(t.profileName, t.accountName) {
		return t.errNoPassword(nil)
	}
	return nil
}

func (t *connectTarget) errNoPSK(cause error) error {
	msg := fmt.Sprintf("no PSK stored for profile %q — add it in the TOMOSIA VPN menu bar app or run `vpn profile add`", t.profileName)
	if cause != nil {
		return fmt.Errorf("%s: %w", msg, cause)
	}
	return errors.New(msg)
}

func (t *connectTarget) errNoPassword(cause error) error {
	msg := fmt.Sprintf("no password stored for account %q — run `vpn account add %s %s`", t.accountName, t.profileName, t.accountName)
	if cause != nil {
		return fmt.Errorf("%s: %w", msg, cause)
	}
	return errors.New(msg)
}

func doConnect(profileName, accountName string, timeout time.Duration, verbose bool, rekeyAfter time.Duration) error {
	tuneDaemonRuntime()
	t, err := resolveTarget(profileName, accountName)
	if err != nil {
		return err
	}
	pName, p, aName := t.profileName, t.profile, t.accountName
	psk, err := keychain.GetPSK(pName)
	if err != nil {
		return t.errNoPSK(err)
	}
	password, err := keychain.GetPassword(pName, aName)
	if err != nil {
		return t.errNoPassword(err)
	}

	return engine.Connect(engine.Config{
		ProfileName:  pName,
		Server:       p.Server,
		ServerID:     p.ServerID,
		AccountName:  aName,
		Password:     password,
		PSK:          psk,
		IKEProposals: p.IKEProposals,
		ESPProposals: p.ESPProposals,
		MTU:          t.mtu,
		FullTunnel:   p.FullTunnel,
		Timeout:      timeout,
		Verbose:      verbose || t.verbose,
		RekeyAfter:   rekeyAfter,
		KillSwitch:   t.killSwitch,
	})
}

func cmdDisconnect(args []string) error {
	return engine.Disconnect()
}

func cmdStatus(args []string) error {
	fs := newFlagSet("status")
	asJSON := fs.Bool("json", false, "output as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	s, err := state.Load()
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	}
	fmt.Printf("Phase:   %s\n", s.Phase)
	if s.Profile != "" {
		fmt.Printf("Profile: %s (account %s)\n", s.Profile, s.Account)
	}
	if s.Server != "" {
		fmt.Printf("Server:  %s\n", s.Server)
	}
	if s.TunDevice != "" {
		fmt.Printf("Tunnel:  %s (%s)\n", s.TunDevice, s.LocalIP)
	}
	if s.Reconnecting {
		fmt.Printf("Reconnecting: yes (lost the tunnel %d time(s); it is being re-established automatically)\n", s.Reconnects)
	} else if s.Reconnects > 0 {
		fmt.Printf("Reconnects: %d\n", s.Reconnects)
	}
	if s.FailStage != "" {
		fmt.Printf("Last failure: %s (%s)\n", s.FailStage, s.FailDetail)
	}
	fmt.Printf("Updated: %s\n", s.UpdatedAt.Format(time.RFC3339))
	printWarnings(s)
	return nil
}

func printWarnings(s *state.State) {
	for _, w := range s.Warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
	}
}

func cmdRepair(args []string) error {
	// repair must work without credentials — it only restores routing/DNS
	// state, it never (re)negotiates the tunnel.
	return engine.Repair()
}

func cmdLogs(args []string) error {
	fs := newFlagSet("logs")
	follow := fs.Bool("f", false, "follow the log file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	logPath := engine.LogPath()
	if _, err := os.Stat(logPath); err != nil {
		return fmt.Errorf("no log file yet at %s (nothing has connected)", logPath)
	}
	// The log file is 0600, owned by whoever ran `connect` (root, via
	// privilege.Elevate) — a plain unprivileged read would fail with
	// permission denied, so briefly elevate just to read/tail it.
	return privilege.Elevate(func() error {
		if *follow {
			c := exec.Command(sysbin.Tail, "-F", logPath) // -F: keep following across a rotation
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		}
		data, err := os.ReadFile(logPath)
		if err != nil {
			return err
		}
		os.Stdout.Write(data)
		return nil
	})
}

// tuneDaemonRuntime shrinks the long-lived background process. It sits idle
// almost all of its life, moving small packets: two OS threads are plenty
// (fewer threads to wake and keep resident), and a GC that runs a little
// earlier keeps the resident heap small instead of letting it drift up.
func tuneDaemonRuntime() {
	if n := runtime.NumCPU(); n > 4 {
		runtime.GOMAXPROCS(4)
	}
	debug.SetGCPercent(100)
	debug.SetMemoryLimit(256 << 20)
}
