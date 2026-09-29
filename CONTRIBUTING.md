# Contributing to TOMOSIA VPN

This guide is for people who build, change or release TOMOSIA VPN. End-user
installation and usage live in [README.md](README.md).

## How the pieces fit

TOMOSIA VPN is two programs:

| Part | Source | Installed at |
|---|---|---|
| `vpn` CLI and connection engine (IKEv1, ESP, L2TP, PPP), in Go | `cmd/vpn`, `internal/` | `/usr/local/bin/vpn`, setuid root |
| Menu bar app, in SwiftUI (one file) | `main.swift` | `/Applications/TOMOSIA-VPN.app` |

The app never talks to the network itself. It runs `vpn` commands and reads the
files the CLI writes:

| File | Written by | Holds |
|---|---|---|
| `~/.config/vpn/config.json` | CLI | Profiles, accounts, MTU, kill switch, verbose flag |
| `/var/run/vpn/state.json` | Connection daemon | Phase (`CONNECTING` / `CONNECTED` / `FAILED` / `DISCONNECTED`), IP, failure stage and detail |
| `/var/log/vpn.log` | CLI and daemon | Log (`vpn logs`) |
| macOS Keychain | CLI | Passwords and pre-shared keys |

The binary is setuid root and raises privilege only around the operations that
need it (routes, DNS, the utun device). `/etc/vpn-owner-uid` records which user
it acts for.

A `.dmg` install has no installer step, so the app carries a copy of `vpn` in
`Contents/Resources` and installs it on first launch after an administrator
prompt (`CLIInstaller` in `main.swift`).

### Repository layout

| Path | What |
|---|---|
| `cmd/vpn` | CLI entry point |
| `cmd/releasesign` | Release signing tool (`keygen`, `sign`) |
| `internal/engine` | Connect / reconnect loop, liveness, rekeying, network events |
| `internal/ike`, `internal/ipsec`, `internal/l2tp`, `internal/ppp` | Protocol stages |
| `internal/routing`, `internal/dnsmgr`, `internal/tun` | Routes, DNS, utun device |
| `internal/cli` | Command-line commands |
| `internal/config`, `internal/state`, `internal/keychain` | Files and secrets described above |
| `internal/release` | Signed release manifest used by `vpn update` |
| `main.swift` | Menu bar app |
| `assets/` | App icon, logo, status item images, and the script that renders them |
| `build.sh`, `make-dmg.sh` | Build the app and the disk image |
| `install.sh`, `install-arm64.sh`, `install-intel.sh`, `install-local.sh`, `uninstall.sh` | Install / uninstall |
| `docs/` | Landing page, published by GitHub Pages |

## Requirements

- macOS on Apple Silicon or Intel.
- Go, at the version in `go.mod` (`brew install go`).
- Xcode 26 (Swift 6). `build.sh` warns on older toolchains; Swift 5.9 is known not to
  compile `main.swift`.

## Build and run

```bash
git clone https://github.com/TOMOSIA-VIETNAM/vpn.git && cd vpn

bash build.sh                 # build/TOMOSIA-VPN.app, with the CLI bundled inside
VERSION=1.2.3 bash build.sh   # stamp a version (default: latest git tag, else 0.0.0)
open build/TOMOSIA-VPN.app

./install-local.sh            # build, then install the CLI and the app on this Mac (sudo)
bash make-dmg.sh              # build/TOMOSIA-VPN.dmg
```

Quitting the app disconnects an active VPN. To stop a test build without touching
the connection, kill it instead: `pkill -9 -f build/TOMOSIA-VPN.app`.

## Checks

CI (`.github/workflows/test.yml`) runs these on every pull request and on pushes to
`main`; run them before opening one:

```bash
gofmt -l .        # must print nothing
go vet ./...
go test ./...
bash build.sh     # the app must compile
```

UI changes have no automated tests: build the app and look at every state you
touched (idle, connecting, connected, failure alerts, sheets), in both Light and
Dark appearance.

## Conventions

### Menu bar app

- Colors and metrics come from `Theme` in `main.swift`, which maps to system colors so
  the app follows Light/Dark mode and the user's accent color. Do not add literal
  colors in views.
- Use native controls (switches, text fields, bordered buttons) unless a stock control
  misbehaves in the popover; `ConnectSwitch` exists because the stock switch turns gray
  when the popover is not key.
- The product name comes from `AppBranding.name`. UI text is English.
- Status item images are templates: black on transparent, so macOS tints them.

### Brand assets

Every raster brand asset is drawn from one vector definition in
`assets/render-icons.swift`:

```bash
swift assets/render-icons.swift
```

It rewrites `assets/AppIcon.icns`, `assets/logo.png`, `docs/logo.png` and
`assets/menubar-*.png`. Change the script, never the PNGs by hand, and commit the
regenerated files with it.

### Scripts

Terminal output is plain English without emoji: one `==> [n/m] step` line per step,
warnings and errors on stderr prefixed `warning:` / `error:`, and a short closing
line with the installed paths.

### Commits and pull requests

- [Conventional Commits](https://www.conventionalcommits.org/) messages: `feat(ui): …`,
  `fix(engine): …`, `docs: …`.
- Once a pull request has review comments, address them in new commits. Do not amend,
  squash or force-push: reviewers would have to start over.

## Releasing

1. Merge to `main`.
2. Tag and push a semver tag:

   ```bash
   git tag v1.2.3
   git push origin v1.2.3
   ```

`.github/workflows/release.yml` runs the test workflow on that commit first and
publishes nothing if it fails.
It builds the CLI for both architectures, `TOMOSIA-VPN.app.zip` and
`TOMOSIA-VPN.dmg`, writes `SHA256SUMS` and signs it (`SHA256SUMS.sig`), then publishes
the release. The tag becomes both `vpn version` and the version in the app footer.

- Signing needs the `RELEASE_SIGNING_KEY` secret: the base64 ed25519 seed from
  `go run ./cmd/releasesign keygen`, matching `release.PublicKey`. `vpn update` refuses
  a release whose signature does not verify against the key compiled into the
  installed binary.
- `install.sh`, the README and the landing page download from
  `releases/latest/download/<asset>`. Renaming an asset breaks those links until a
  release that carries the new name is published.

The landing page in `docs/` is published by `.github/workflows/pages.yml` on every
push to `main` that touches `docs/`.
