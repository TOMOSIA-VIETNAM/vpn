<p align="center">
  <img src="assets/logo.png" width="88" height="88" alt="TOMOSIA VPN logo" />
</p>

<h1 align="center">TOMOSIA VPN</h1>

<p align="center">切れても自動でつながる社内 VPN。</p>

<p align="center">
  <a href="https://vpntms.vercel.app/ja"><b>ウェブサイト</b></a>
  &nbsp;·&nbsp;
  <a href="https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download/TOMOSIA-VPN.dmg"><b>ダウンロード</b></a>
  &nbsp;·&nbsp;
  <a href="https://github.com/TOMOSIA-VIETNAM/vpn/releases">リリース</a>
  &nbsp;·&nbsp;
  <a href="CONTRIBUTING.md">開発者向け</a>
</p>

<p align="center"><sub><a href="README.md">English</a> · <a href="README.vi.md">Tiếng Việt</a> · <b>日本語</b></sub></p>

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
    <img src="assets/screenshots/popover-light.png" width="380" alt="メニューバーの TOMOSIA VPN" />
  </picture>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/screenshots/settings-dark.png" />
    <img src="assets/screenshots/settings-light.png" height="440" alt="設定" />
  </picture>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/screenshots/new-configuration-dark.png" />
    <img src="assets/screenshots/new-configuration-light.png" height="440" alt="VPN 構成の追加" />
  </picture>
</p>

## インストール

1. [`TOMOSIA-VPN.dmg`](https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download/TOMOSIA-VPN.dmg) をダウンロードします。
2. 開いて **TOMOSIA VPN** を **アプリケーション** へドラッグします。
3. アプリを開きます。初回は macOS にブロックされることがあります。下の [macOS にブロックされたら](#macos-にブロックされたら) を参照してください（1 回だけ、1 分もかかりません）。

動作環境：macOS 12 以降。

### macOS にブロックされたら

TOMOSIA VPN は有料の Apple Developer ID で署名されていないため、初回起動は macOS にブロックされます（"Apple could not verify…"）。**App Store と確認済みの開発元** を選んでいても同様です。アプリは安全です。1 回だけ許可してください。

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/screenshots/gatekeeper-steps-ja-dark.png" />
  <img src="assets/screenshots/gatekeeper-steps-ja-light.png" alt="macOS が初回起動をブロック：1 Done をクリック、2 プライバシーとセキュリティで Open Anyway、3 もう一度 Open Anyway、4 Mac のパスワードを入力" />
</picture>

## アンインストール

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/uninstall.sh | bash
```

---

開発者向け：[CONTRIBUTING.md](CONTRIBUTING.md)
