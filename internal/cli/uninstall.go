package cli

import (
	"bufio"
	"fmt"
	"os"

	"vpn/internal/config"
	"vpn/internal/engine"
	"vpn/internal/keychain"
	"vpn/internal/privilege"
	"vpn/internal/state"
	"vpn/internal/vpnlog"
)

func cmdUninstall(args []string) error {
	fs := newFlagSet("uninstall")
	yes := fs.Bool("y", false, "don't ask for confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !*yes {
		fmt.Println("This will remove: " + installPath + ", the log, state, and every saved profile/account (including PSK/passwords in Keychain).")
		fmt.Print("Proceed? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		if line != "y\n" && line != "Y\n" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Tear down a live tunnel first — otherwise routes/DNS are left half
	// overridden with nothing left around to ever restore them.
	if st, err := state.Load(); err == nil && (st.Phase == state.PhaseConnected || st.Phase == state.PhaseConnecting) {
		fmt.Println("Disconnecting...")
		if err := engine.Disconnect(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: disconnect failed, continuing with removal: %v\n", err)
		}
	}

	// Every profile/account's Keychain entry — config.Load never errors
	// on a missing file, so this is safe even on a barely-initialized
	// install.
	if cfg, err := config.Load(); err == nil {
		for profileName, p := range cfg.Profiles {
			_ = keychain.DeletePSK(profileName)
			for acctName := range p.Accounts {
				_ = keychain.DeletePassword(profileName, acctName)
			}
		}
	}

	// The user's own config dir (~/.config/vpn) — unprivileged, it's in
	// their own home directory.
	if dir, err := config.Dir(); err == nil {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", dir, err)
		}
	}

	// Everything else (log, state dir, the binary itself) is root-owned.
	err := privilege.Elevate(func() error {
		if err := os.RemoveAll(state.Dir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", state.Dir, err)
		}
		for _, p := range []string{vpnlog.Path, vpnlog.RotatedPath} {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", p, err)
			}
		}
		// Delete the binary itself — safe on Unix even though it's the file
		// this running process's own image was exec'd from (removing a
		// directory entry doesn't touch an already-open/already-mapped
		// inode, so this process keeps running fine).
		if err := os.Remove(installPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", installPath, err)
		}
		// OwnerFile only after the binary is gone: a setuid binary left
		// without it refuses to run at all (see privilege.CheckOwner), so a
		// failed removal above must not also strand the user with a
		// binary they can no longer uninstall.
		if err := os.Remove(privilege.OwnerFile); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", privilege.OwnerFile, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Println("vpn has been fully removed.")
	if app, ok := installedApp(); ok {
		fmt.Printf("The menu bar app is still at %s — run uninstall.sh to remove it too, or move it to the Trash.\n", app)
	}
	return nil
}
