<p align="center">
  <img src="assets/logo.png" width="96" height="96" alt="TOMOSIA VPN logo" />
</p>

<h1 align="center">TOMOSIA VPN</h1>

<p align="center">
  <a href="README.md">Tiếng Việt</a> · <b>English</b> · <a href="README.ja.md">日本語</a>
</p>

<p align="center">
  L2TP/IPsec VPN for macOS, in the menu bar — steady connections, clear errors.
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-v0.6.1-blue.svg" alt="Version 0.6.1" />
  <img src="https://img.shields.io/badge/macOS-12.0+-black.svg" alt="macOS 12+" />
  <img src="https://img.shields.io/badge/arch-Apple%20Silicon%20%7C%20Intel-success.svg" alt="Architecture" />
</p>

---

## About

The L2TP VPN built into macOS (*System Settings → VPN*) often drops on office or public Wi-Fi behind a firewall, and when it fails it says little about why.

TOMOSIA VPN replaces it:

- Turn the VPN on and off with one switch in the menu bar; keep several configurations.
- Reconnects by itself when the connection drops, and tells you when it drops and when it is back.
- Specific errors: wrong password, wrong shared secret, a network that blocks VPN…
- Passwords and shared secrets are kept in the macOS Keychain, never in a config file.

Requires macOS 12 or later, on Apple Silicon or Intel.

---

## Installation

### Option 1 — .dmg file (recommended)

1. Download [`TOMOSIA-VPN.dmg`](https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download/TOMOSIA-VPN.dmg) (always the latest release).
2. Open it and drag **TOMOSIA VPN** into **Applications**.
3. Open the app. The first time:
   - If macOS says the developer cannot be verified: right-click the app → **Open**.
   - macOS asks once for an administrator password to install the connection helper (`/usr/local/bin/vpn`).
   - macOS asks to allow notifications: choose **Allow** to be told when the VPN drops.

### Option 2 — One command in Terminal

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install.sh | bash
```

The script detects Apple Silicon or Intel, installs the app and the helper, then opens the app.

---

## Getting started

Ask your network administrator for 4 things: the **server address**, the **shared secret (PSK)**, your **account name** and **password**.

1. Click the TOMOSIA VPN icon in the menu bar.
2. Click **+** next to **Configurations**.
3. Fill in:
   - **Display name**: any name, e.g. `Office`.
   - **Server address**: the VPN's IP address or host name.
   - **Account name** and **Password**: your VPN login.
   - **Shared secret**: the IPsec pre-shared key.
   - **Send all traffic over VPN**: on to send all internet traffic through the VPN; off to send only company network traffic.
4. Click **Create**, then turn on the configuration's switch to connect.

To edit or delete a configuration, click **…** on its row, or right-click it. Turn the VPN off first.

### Menu bar icon

| Icon | Meaning |
|---|---|
| Solid shield | Connected |
| Dimmed outline shield | Connecting |
| Outline shield | Not connected |
| Outline shield with `!` | Error: connecting failed, or the VPN dropped and is waiting to reconnect |

When the VPN drops or comes back, the app posts a macOS notification. If you see none: open the app → gear icon (**Settings**) → **Notifications** → **Open Settings…** and allow notifications for TOMOSIA VPN.

### Settings

The gear icon at the bottom of the popover. Changes apply on the next connection.

- **MTU**: keep `1280` (the default). Choose `1400` only if your administrator asks.
- **Kill switch**: when a "Send all traffic" VPN drops, block the internet until it reconnects, so nothing leaves outside the VPN.
- **Verbose log**: detailed logs for your administrator when investigating a problem.
- **Notifications**: whether the app may post notifications.

---

## Troubleshooting

The alert card in the popover names the cause and offers **Try Again** / **Update Password**.

| Message | Cause | What to do |
|---|---|---|
| Authentication Failed | Wrong account name or password | Click **Update Password** and enter it again |
| IKE Handshake Failed | Wrong shared secret | **…** → **Edit…**, enter the shared secret again |
| Server Not Responding | This network blocks VPN (UDP 500/4500), or the server is down | Try another network (e.g. mobile hotspot), or ask your administrator |
| Waiting for Network | This Mac is offline | The VPN reconnects once the network is back |
| VPN Stopped Unexpectedly | The VPN process stopped | Turn the switch on again |

Still cannot connect? Open Terminal and run:

```bash
vpn diagnose    # checks the network path, DNS and UDP ports 500/4500
vpn repair      # restores routes/DNS if the network misbehaves after a drop
```

Send the output of `vpn diagnose` and `vpn logs` to your administrator.

---

## Updating and uninstalling

**Update:** download the latest `.dmg` and drag the app into **Applications** again, or re-run the one-line install command above. The current version is shown at the bottom of the popover (e.g. `v0.6.1`).

**Uninstall** (removes the app, the helper, your configurations and the passwords saved in Keychain):

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/uninstall.sh | bash
```

---

## Command line (advanced)

Everything the app does is also available through the `vpn` command:

```bash
vpn profile add work --server vpn.example.com   # asks for the shared secret
vpn account add work alice --default            # asks for the password
vpn connect
vpn status
vpn disconnect
```

| Command | Does |
|---|---|
| `vpn connect [--profile <name>]` / `vpn disconnect` | Connect / disconnect |
| `vpn status` | State and IP address |
| `vpn profile list` | Configurations (`*` = selected) |
| `vpn profile add <name> --server <host>` | Add a configuration |
| `vpn profile edit <name> [--server host] [--user name] [--full-tunnel=bool] [--set-psk]` | Edit a configuration, keeping saved secrets |
| `vpn profile rename <name> [display name]` | Change the display name |
| `vpn profile remove <name>` | Delete a configuration and its Keychain secrets |
| `vpn account add <configuration> <user>` | Add an account |
| `vpn mtu [1280\|1400]`, `vpn killswitch [on\|off]`, `vpn verbose [on\|off]` | Global settings |
| `vpn diagnose`, `vpn logs -f`, `vpn repair` | Diagnose and repair the network |
| `vpn update` | Update the helper (signature checked) |
| `vpn version` | Version |

---

## Contributing

Building from source, tests and releases: see [CONTRIBUTING.md](CONTRIBUTING.md).
