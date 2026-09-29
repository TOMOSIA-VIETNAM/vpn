<p align="center">
  <img src="assets/logo.png" width="88" height="88" alt="TOMOSIA VPN logo" />
</p>

<h1 align="center">TOMOSIA VPN</h1>

<p align="center">The company VPN that reconnects by itself.</p>

<p align="center">
  <a href="https://vpntms.vercel.app/en"><b>Website</b></a>
  &nbsp;·&nbsp;
  <a href="https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download/TOMOSIA-VPN.dmg"><b>Download</b></a>
  &nbsp;·&nbsp;
  <a href="https://github.com/TOMOSIA-VIETNAM/vpn/releases">Releases</a>
  &nbsp;·&nbsp;
  <a href="CONTRIBUTING.md">For developers</a>
</p>

<p align="center"><sub><b>English</b> · <a href="README.vi.md">Tiếng Việt</a> · <a href="README.ja.md">日本語</a></sub></p>

<p align="center">
  <a href="https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest"><img src="https://img.shields.io/github/v/release/TOMOSIA-VIETNAM/vpn?label=release&color=blue" alt="Latest release" /></a>
  <a href="https://github.com/TOMOSIA-VIETNAM/vpn/actions/workflows/test.yml"><img src="https://img.shields.io/github/actions/workflow/status/TOMOSIA-VIETNAM/vpn/test.yml?branch=main&label=tests" alt="Tests" /></a>
  <img src="https://img.shields.io/badge/macOS-12%2B-black?logo=apple" alt="macOS 12+" />
  <img src="https://img.shields.io/badge/Apple%20Silicon%20%7C%20Intel-universal-555" alt="Apple Silicon and Intel" />
  <img src="https://img.shields.io/badge/L2TP%2FIPsec-VPN-555" alt="L2TP/IPsec" />
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/screenshots/popover-dark.png" />
    <img src="assets/screenshots/popover-light.png" width="380" alt="TOMOSIA VPN in the menu bar" />
  </picture>
</p>

<table align="center">
  <tr>
    <td align="center" valign="top" width="50%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="assets/screenshots/settings-dark.png" />
        <img src="assets/screenshots/settings-light.png" width="100%" alt="Settings" />
      </picture>
      <br /><sub>Settings</sub>
    </td>
    <td align="center" valign="top" width="50%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="assets/screenshots/new-configuration-dark.png" />
        <img src="assets/screenshots/new-configuration-light.png" width="100%" alt="Adding a VPN configuration" />
      </picture>
      <br /><sub>Adding a VPN configuration</sub>
    </td>
  </tr>
</table>

## Install

1. Download [`TOMOSIA-VPN.dmg`](https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download/TOMOSIA-VPN.dmg).
2. Open it and drag **TOMOSIA VPN** into **Applications**.
3. Open the app (if macOS blocks it: right-click the app → **Open**).

Requires macOS 12 or later.

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/uninstall.sh | bash
```

---

For developers: [CONTRIBUTING.md](CONTRIBUTING.md).
