<p align="center">
  <img src="assets/logo.png" width="88" height="88" alt="TOMOSIA VPN logo" />
</p>

<h1 align="center">TOMOSIA VPN</h1>

<p align="center">VPN công ty tự kết nối lại khi rớt mạng.</p>

<p align="center">
  <a href="https://vpntms.vercel.app/vi"><b>Trang web</b></a>
  &nbsp;·&nbsp;
  <a href="https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download/TOMOSIA-VPN.dmg"><b>Tải về</b></a>
  &nbsp;·&nbsp;
  <a href="https://github.com/TOMOSIA-VIETNAM/vpn/releases">Bản phát hành</a>
  &nbsp;·&nbsp;
  <a href="CONTRIBUTING.md">Dành cho người phát triển</a>
</p>

<p align="center"><sub><a href="README.md">English</a> · <b>Tiếng Việt</b> · <a href="README.ja.md">日本語</a></sub></p>

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
    <img src="assets/screenshots/popover-light.png" width="380" alt="TOMOSIA VPN trên thanh Menu Bar" />
  </picture>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/screenshots/settings-dark.png" />
    <img src="assets/screenshots/settings-light.png" height="440" alt="Cài đặt" />
  </picture>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/screenshots/new-configuration-dark.png" />
    <img src="assets/screenshots/new-configuration-light.png" height="440" alt="Thêm cấu hình VPN" />
  </picture>
</p>

## Cài đặt

1. Tải [`TOMOSIA-VPN.dmg`](https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download/TOMOSIA-VPN.dmg).
2. Mở file, kéo **TOMOSIA VPN** vào **Applications**.
3. Mở app. Lần đầu macOS thường sẽ chặn — xem [Nếu macOS chặn app](#nếu-macos-chặn-app) bên dưới (chỉ làm một lần, chưa đến một phút).

Yêu cầu macOS 12 trở lên.

### Nếu macOS chặn app

TOMOSIA VPN chưa được ký bằng Apple Developer ID trả phí nên macOS chặn lần mở đầu ("Apple could not verify…"), kể cả khi đã bật **App Store and Known Developers**. App an toàn — chỉ cần cho phép một lần:

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/screenshots/gatekeeper-steps-vi-dark.png" />
  <img src="assets/screenshots/gatekeeper-steps-vi-light.png" alt="macOS chặn lần mở đầu: 1 bấm Done, 2 vào Privacy & Security và bấm Open Anyway, 3 bấm Open Anyway lần nữa, 4 nhập mật khẩu máy Mac" />
</picture>

## Gỡ cài đặt

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/uninstall.sh | bash
```

---

Dành cho người phát triển: [CONTRIBUTING.md](CONTRIBUTING.md).
