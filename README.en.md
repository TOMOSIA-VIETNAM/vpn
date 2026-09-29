<p align="center">
  <img src="assets/logo.png" width="96" height="96" alt="TOMOSIA VPN logo" />
</p>

<h1 align="center">TOMOSIA VPN</h1>

<p align="center">
  <a href="README.md">Tiếng Việt</a> · <b>English</b> · <a href="README.ja.md">日本語</a>
</p>

<p align="center">
  <b>A pure macOS L2TP/IPsec VPN client — stable, secure, and free of third-party dependencies.</b>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-v0.6.1-blue.svg" alt="Version 0.6.1" />
  <img src="https://img.shields.io/badge/macOS-12.0+-black.svg" alt="macOS 12+" />
  <img src="https://img.shields.io/badge/arch-Apple%20Silicon%20%7C%20Intel-success.svg" alt="Architecture" />
  <img src="https://img.shields.io/badge/protocol-L2TP%20%2F%20IPsec-orange.svg" alt="L2TP/IPsec" />
</p>

---

## 💡 Introduction

The built-in macOS VPN (*System Settings → VPN → L2TP over IPsec*) is often flaky, struggles on public Wi-Fi or office networks behind firewalls, and reports only generic errors when something goes wrong.

**TOMOSIA VPN** implements the whole protocol stack itself (IKEv1, ESP, L2TP, PPP) for steady, reliable and easy-to-diagnose connections:
- 🖥️ **Menu bar app (SwiftUI)**: connect or disconnect with one click, manage multiple profiles and accounts, see status instantly.
- ⚡ **CLI engine (`vpn`)**: a high-performance connection manager written in Go — fully self-contained, with no Docker, WireGuard, strongSwan, xl2tpd or pppd.

---

## 🚀 Installation

**Option 1 — `.dmg` (like any other Mac app):**

1. Download [`TMS-VPN.dmg`](https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download/TMS-VPN.dmg) (always the latest release).
2. Open it and drag **TOMOSIA VPN** into **Applications**.
3. Launch the app. On first launch macOS asks for your administrator password once to install the `vpn` core. If macOS warns about an unverified developer: right-click the app → **Open**.

Build the `.dmg` yourself: `VERSION=1.2.3 ./make-dmg.sh` (output: `build/TMS-VPN.dmg`).

**Option 2 — Script:**

**Automatic install (detects Apple Silicon M1/M2/M3… or Intel Macs from 2017+):**

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install.sh | bash
```

**Or pick the architecture yourself:**

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install-arm64.sh | bash   # Apple Silicon (M1/M2/M3...)
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install-intel.sh | bash   # Intel Mac
```

Verify the installation:
```bash
vpn version    # Output: vpn v0.6.1
```
The menu bar app shows its version at the bottom left of the popover (e.g. `v0.6.1`, next to the copyright line).

---

## 🎯 Getting started

Get four details from your network administrator: **server address**, **IPsec pre-shared key (PSK)**, **username** and **password**.

### Option 1: Menu bar app (recommended)

1. Open **TOMOSIA VPN** from `Applications` or Spotlight.
2. Click the TOMOSIA VPN icon in the menu bar → **Add**.
3. Fill in:
   - **Display name**: a memorable name (e.g. `Office`).
   - **Server address**: the VPN server's IP or domain.
   - **Account name & Password**: your login.
   - **Shared secret (PSK)**: the IPsec shared key.
4. Click **Create**, then **flip the switch** to connect!

> 🔒 *Passwords and the PSK are stored securely in the macOS Keychain, never in plain text in a config file.*

---

### Option 2: Command line (CLI)

For developers, or for automation via scripts:

```bash
# 1. Add a server profile (the CLI prompts for the PSK securely)
vpn profile add work --server vpn.example.com

# 2. Add a user account (the CLI prompts for the password)
vpn account add work alice --default

# 3. Connect (runs in the background and returns right away)
vpn connect

# 4. Check status & IP
vpn status
curl -4 https://ifconfig.co
```

---

## ✨ Highlights

- 🛡️ **Gets through difficult networks (NAT-T / port switching)**: automatically changes the IKE port when an office router blocks UDP/500 (IPsec passthrough issues), supports NAT-T over UDP/4500 and adapts the MTU.
- 🔄 **Always-on with automatic reconnect**: answers IKE DPD, L2TP Hello and PPP LCP Echo automatically, and re-keys in the background without dropping your session.
- 🌐 **Smart DNS & routing**: configures DNS per VPN, supports Private DNS priority, and flushes the macOS DNS cache on connect/disconnect.
- 🛑 **Kill switch**: optionally blocks all outgoing traffic while a full-tunnel VPN is down and reconnecting.
- 🔐 **System-level security**:
  - Sensitive memory (IKE/ESP/DH keys, passwords) is zeroed after use.
  - Least privilege: the binary raises its rights only for the network/DNS/utun operation that needs them, then drops them immediately.
  - Load limits against DoS / parser flooding.
- 🔒 **Profile lock**: a connected profile can't be edited or deleted, in both the app and the CLI.

---

## 📋 CLI reference

| Task | Command | Notes |
|---|---|---|
| **Connect / disconnect** | `vpn connect` | Connect the active profile |
| | `vpn connect --profile <name> --force` | Force-reconnect a given profile |
| | `vpn disconnect` | Disconnect |
| | `vpn status` | Status, tunnel IP, uptime |
| **Profiles** | `vpn profile list` | List profiles (* = active) |
| | `vpn profile add <name> --server <host>` | Add a server |
| | `vpn profile rename <name> [new name]` | Change the display name shown in the app |
| | `vpn profile edit <name> [--server host] [--user name] [--full-tunnel=bool] [--set-psk]` | Change host / username / tunnel mode; stored password and secret are kept |
| | `vpn profile remove <name>` | Remove a profile and its Keychain secrets (not possible while connected) |
| **Accounts** | `vpn account add <profile> <user>` | Add an account to a profile |
| **Global settings** | `vpn mtu [1280\|1400]` | Set the MTU (1280 for congested 4G/PPPoE) |
| | `vpn killswitch [on\|off]` | Block traffic when the connection drops |
| | `vpn verbose [on\|off]` | Detailed protocol logging |
| **Diagnostics & rescue** | `vpn diagnose` | Full check of connection, DNS, ports 500/4500 |
| | `vpn logs -f` | Follow the log live |
| | `vpn repair` | Clean up leftover routes / DNS after a network error |
| **Update / remove** | `vpn update` | Download and install the latest release |
| | `vpn uninstall` | Remove the CLI, config and Keychain entries |

---

## 🛠️ Troubleshooting

When something goes wrong, run:
```bash
vpn diagnose      # Check the path, DNS and UDP ports 500 / 4500
vpn logs -f       # Follow protocol details
```

### Common errors

| Error message | Cause | Fix |
|---|---|---|
| `IKE_AUTH_FAILED` / `HASH_R mismatch` | Wrong pre-shared key (PSK) | Run `vpn profile add <name> --server <host>` to re-enter the PSK |
| `PPP_AUTH_FAILURE` / `CHAP rejected` | Wrong username or password | Run `vpn account add <profile> <user>` to re-enter the password |
| `IKE_TIMEOUT` / `no response` | Network blocks UDP 500/4500, or wrong server IP | Run `vpn diagnose`; try another network or 4G |
| `already logged in` | The server hasn't released the previous session | Wait 10–15 seconds and reconnect |
| `DNS_FAILURE` / `ROUTE_FAILURE` | Network conflict or leftover DNS | Run `vpn repair` to restore network settings |

---

## 📦 Updating & uninstalling

- **Update**:
  ```bash
  vpn update          # Verifies the ed25519 signature and updates the CLI
  ```
  *(To update the menu bar app too, re-run the one-line installer above. Compare the version in the app footer with `vpn version` to make sure both match.)*

- **Uninstall completely**:
  ```bash
  curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/uninstall.sh | bash
  ```

---

## 💻 For developers

### Requirements
- **Go**: 1.22+ (`brew install go`)
- **Xcode / Swift**: Swift 6 (Xcode 16+) to build `main.swift`

### Build and test
```bash
# Clone the source
git clone https://github.com/TOMOSIA-VIETNAM/vpn.git && cd vpn

# Build the CLI and install it setuid-root
go build -o vpn ./cmd/vpn
sudo install -o root -g wheel -m 4755 vpn /usr/local/bin/vpn
id -u | sudo tee /etc/vpn-owner-uid >/dev/null && sudo chmod 600 /etc/vpn-owner-uid

# Build the menu bar app (universal arm64 + Intel)
# The version shown in the app comes from VERSION; if unset, the latest git tag
# is used (no valid tag → 0.0.0)
bash build.sh                   # or: VERSION=0.6.1 bash build.sh
ditto "build/TMS VPN.app" "/Applications/TMS VPN.app"

# Run unit tests
go test ./...
```

### Release process
Create a new version tag and push it to GitHub to trigger CI/CD, which builds and signs the release. The tag is stamped into both the CLI (`vpn version`) and the app (popover footer, `CFBundleShortVersionString`):
```bash
git tag v0.6.1
git push origin v0.6.1
```

---

## 📄 License & contributing
Built for internal use and the wider macOS community. Pull requests and issues are welcome!
