// Command vpn is a native macOS L2TP/IPsec VPN client: multi-profile,
// multi-account, no Docker/WireGuard/strongSwan/xl2tpd/pppd dependency.
package main

import (
	"fmt"
	"os"

	"vpn/internal/cli"
	"vpn/internal/privilege"
)

var (
	version = "dev" // override at build time via -ldflags "-X main.version=..."
)

func main() {
	// Absolute first thing: reject any other local user before this
	// setuid-root binary does anything else, privileged or not — see
	// privilege.CheckOwner's doc comment for why. Reads privilege.OwnerFile
	// at runtime, not a build-time constant, so this same binary works
	// whether it was compiled locally or downloaded as a prebuilt release.
	if err := privilege.CheckOwner(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	// Must run before any subcommand dispatch: if this binary is installed
	// setuid-root, every subcommand should start unprivileged by default
	// and only the specific operations that need root (see
	// internal/privilege) ever regain it.
	privilege.Drop()

	cli.Version = version
	os.Exit(cli.Run(os.Args[1:]))
}
