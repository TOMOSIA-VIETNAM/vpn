<p align="center">
  <img src="assets/logo.png" width="96" height="96" alt="TMS VPN logo" />
</p>

<h1 align="center">TMS VPN</h1>

<p align="center">
  <b>VPN Client L2TP/IPsec thuần macOS — Ổn định, bảo mật, không phụ thuộc phần mềm ngoài.</b>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-v0.5.3-blue.svg" alt="Version 0.5.3" />
  <img src="https://img.shields.io/badge/macOS-12.0+-black.svg" alt="macOS 12+" />
  <img src="https://img.shields.io/badge/arch-Apple%20Silicon%20%7C%20Intel-success.svg" alt="Architecture" />
  <img src="https://img.shields.io/badge/protocol-L2TP%20%2F%20IPsec-orange.svg" alt="L2TP/IPsec" />
</p>

---

## 💡 Giới thiệu

VPN mặc định của macOS (*System Settings → VPN → L2TP over IPsec*) thường xuyên gặp tình trạng chập chờn, khó kết nối trên các mạng Wi-Fi công cộng/văn phòng có tường lửa, và khi lỗi chỉ báo thông điệp chung chung không rõ nguyên nhân.

**TMS VPN** tự cài đặt trực tiếp toàn bộ giao thức (IKEv1, ESP, L2TP, PPP) giúp kết nối xuyên suốt, ổn định và dễ chẩn đoán:
- 🖥️ **Menu Bar App (SwiftUI)**: Giao diện trực quan trên thanh Menu Bar — kết nối/ngắt kết nối 1 click, quản lý nhiều profile/account, xem IP & trạng thái tức thì.
- ⚡ **CLI Engine (`vpn`)**: Bộ điều phối kết nối hiệu năng cao viết bằng Go — độc lập hoàn toàn, không cần Docker, WireGuard, strongSwan, xl2tpd hay pppd.

---

## 🚀 Cài đặt

**Cài đặt tự động (tự nhận diện chip Apple Silicon M1/M2/M3... hoặc Mac Intel 2017+):**

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install.sh | bash
```

**Hoặc cài đặt chỉ định theo từng kiến trúc:**

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install-arm64.sh | bash   # Apple Silicon (M1/M2/M3...)
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install-intel.sh | bash   # Mac Intel
```

Kiểm tra xác nhận cài đặt thành công:
```bash
vpn version    # Kết quả: vpn v0.5.3
```
Phiên bản App Menu Bar hiển thị ở góc dưới bên trái popover (VD: `TMS VPN Client v0.5.3`).

---

## 🎯 Bắt đầu sử dụng

Chuẩn bị 4 thông tin từ quản trị mạng: **Server Address**, **IPsec Pre-Shared Key (PSK)**, **Username** và **Password**.

### Cách 1: Sử dụng App Menu Bar (Khuyến nghị)

1. Mở ứng dụng **TMS VPN** từ thư mục `Applications` hoặc Spotlight.
2. Click biểu tượng TMS VPN trên Menu Bar → chọn **Add**.
3. Nhập thông tin:
   - **Display name**: Tên gợi nhớ (VD: `Công ty`).
   - **Server address**: Địa chỉ IP hoặc tên miền VPN.
   - **Account name & Password**: Tài khoản đăng nhập của bạn.
   - **Shared secret (PSK)**: Khóa chia sẻ IPsec.
4. Bấm **Create** và **bật công tắc (Toggle)** để kết nối!

> 🔒 *Mật khẩu và PSK được mã hóa và lưu trữ an toàn trong macOS Keychain, không lưu plain-text trong file cấu hình.*

---

### Cách 2: Sử dụng dòng lệnh (CLI)

Thích hợp cho developer hoặc chạy tự động qua script:

```bash
# 1. Thêm profile server (CLI sẽ hỏi nhập PSK bảo mật)
vpn profile add work --server vpn.example.com

# 2. Thêm tài khoản người dùng (CLI sẽ hỏi nhập mật khẩu)
vpn account add work nguyenvana --default

# 3. Kết nối (chạy nền, trả về kết quả ngay)
vpn connect

# 4. Kiểm tra trạng thái & IP
vpn status
curl -4 https://ifconfig.co
```

---

## ✨ Tính năng nổi bật

- 🛡️ **Vượt mạng khó (NAT-T / Port Switching)**: Tự động đổi cổng IKE khi gặp router văn phòng chặn UDP/500 (lỗi IPsec passthrough), hỗ trợ NAT-T qua UDP/4500 và tự điều chỉnh MTU.
- 🔄 **Giữ kết nối liên tục & Tự động kết nối lại**: Tự động phản hồi IKE DPD, L2TP Hello và PPP LCP Echo. Tự động gia hạn khóa mã hóa (Rekeying) định kỳ trong nền mà không làm rớt phiên làm việc.
- 🌐 **Smart DNS & Routing**: Tự động cấu hình DNS theo VPN, hỗ trợ Private DNS Priority và tự động flush DNS cache của macOS khi kết nối/ngắt kết nối.
- 🛑 **Kill Switch**: Tùy chọn chặn toàn bộ traffic ra ngoài khi VPN full-tunnel bị đứt và đang tự kết nối lại.
- 🔐 **Bảo mật cấp hệ thống**:
  - Tự động xóa sạch bộ nhớ nhạy cảm (Memory Zeroization) cho khóa IKE/ESP/DH và mật khẩu.
  - Phân quyền tối thiểu: binary chỉ tạm nâng quyền đúng lúc cần thao tác network/DNS/utun rồi hạ quyền ngay.
  - Giới hạn tải chống tấn công DoS / Parser Flooding.

---

## 📋 Tra cứu lệnh CLI thông dụng

| Thao tác | Lệnh | Ghi chú |
|---|---|---|
| **Kết nối / Ngắt** | `vpn connect` | Kết nối profile đang active |
| | `vpn connect --profile <tên> --force` | Ép kết nối lại profile chỉ định |
| | `vpn disconnect` | Ngắt kết nối hiện tại |
| | `vpn status` | Xem trạng thái, IP tunnel, thời gian kết nối |
| **Quản lý Profile** | `vpn profile list` | Danh sách profile (* = active) |
| | `vpn profile add <tên> --server <host>` | Thêm server mới |
| | `vpn profile rename <tên> [tên mới]` | Đổi tên hiển thị trên App |
| | `vpn profile remove <tên>` | Xóa profile và secret trong Keychain |
| **Quản lý Account** | `vpn account add <profile> <user>` | Thêm tài khoản cho profile |
| **Cài đặt chung** | `vpn mtu [1280\|1400]` | Đặt MTU (1280 cho mạng 4G/PPPoE hay nghẽn) |
| | `vpn killswitch [on\|off]` | Bật/tắt bảo vệ ngắt mạng khi rớt kết nối |
| | `vpn verbose [on\|off]` | Bật/tắt log chi tiết giao thức |
| **Chẩn đoán & Cứu trợ** | `vpn diagnose` | Kiểm tra toàn diện kết nối, DNS, cổng 500/4500 |
| | `vpn logs -f` | Xem log trực tiếp theo thời gian thực |
| | `vpn repair` | Dọn dẹp route / DNS nếu bị lỗi mạng tồn dư |
| **Cập nhật / Gỡ bỏ** | `vpn update` | Tự động tải & cập nhật bản phát hành mới nhất |
| | `vpn uninstall` | Gỡ bỏ CLI, cấu hình và Keychain |

---

## 🛠️ Xử lý sự cố (Troubleshooting)

Khi gặp lỗi, chạy lệnh sau để kiểm tra:
```bash
vpn diagnose      # Kiểm tra đường truyền, DNS và cổng UDP 500 / 4500
vpn logs -f       # Xem chi tiết gói tin giao thức
```

### Các lỗi thường gặp:

| Lỗi thông báo | Nguyên nhân | Cách khắc phục |
|---|---|---|
| `IKE_AUTH_FAILED` / `HASH_R mismatch` | Sai Pre-Shared Key (PSK) | Chạy `vpn profile add <tên> --server <host>` để nhập lại PSK |
| `PPP_AUTH_FAILURE` / `CHAP rejected` | Sai Username hoặc Password | Chạy `vpn account add <profile> <user>` để nhập lại mật khẩu |
| `IKE_TIMEOUT` / `no response` | Mạng chặn cổng UDP 500/4500 hoặc sai IP server | Chạy `vpn diagnose` kiểm tra; thử đổi sang mạng khác hoặc 4G |
| `already logged in` | Phiên kết nối cũ trên server chưa giải phóng | Đợi khoảng 10-15 giây rồi thử kết nối lại |
| `DNS_FAILURE` / `ROUTE_FAILURE` | Xung đột mạng hoặc DNS cũ còn sót | Chạy `vpn repair` để khôi phục cấu hình mạng |

---

## 📦 Cập nhật & Gỡ cài đặt

- **Cập nhật phiên bản mới**:
  ```bash
  vpn update          # Tự động kiểm tra chữ ký số ed25519 và cập nhật CLI
  ```
  *(Để cập nhật cả App Menu Bar, chạy lại lệnh cài đặt nhanh 1 dòng ở đầu trang. So sánh phiên bản ở footer của App với `vpn version` để chắc chắn cả hai đã cùng bản).*

- **Gỡ cài đặt hoàn toàn**:
  ```bash
  curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/uninstall.sh | bash
  ```

---

## 💻 Dành cho Developer

### Yêu cầu môi trường
- **Go**: 1.22+ (`brew install go`)
- **Xcode / Swift**: Swift 6 (Xcode 16+) để biên dịch `main.swift`

### Biên dịch và kiểm thử
```bash
# Clone source code
git clone https://github.com/TOMOSIA-VIETNAM/vpn.git && cd vpn

# Build CLI và cài đặt với quyền setuid-root
go build -o vpn ./cmd/vpn
sudo install -o root -g wheel -m 4755 vpn /usr/local/bin/vpn
id -u | sudo tee /etc/vpn-owner-uid >/dev/null && sudo chmod 600 /etc/vpn-owner-uid

# Build Menu Bar App (Universal arm64 + Intel)
# Phiên bản hiển thị trên App lấy từ biến VERSION; bỏ trống thì dùng git tag
# mới nhất (không có tag hợp lệ → 0.0.0)
bash build.sh                   # hoặc: VERSION=0.5.3 bash build.sh
ditto "build/TMS VPN.app" "/Applications/TMS VPN.app"

# Chạy Unit Tests
go test ./...
```

### Quy trình Release (v0.5.3)
Tạo tag phiên bản mới và push lên GitHub để kích hoạt CI/CD tự động build & ký chữ ký số. Tag này được gắn vào cả CLI (`vpn version`) và App (footer popover, `CFBundleShortVersionString`):
```bash
git tag v0.5.3
git push origin v0.5.3
```

---

## 📄 Bản quyền & Đóng góp
Dự án được phát triển cho nội bộ và cộng đồng sử dụng macOS. Mọi đóng góp (Pull Request / Issue) đều được hoan nghênh!
