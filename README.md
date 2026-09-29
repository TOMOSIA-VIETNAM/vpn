<p align="center">
  <img src="assets/logo.png" width="96" height="96" alt="TOMOSIA VPN logo" />
</p>

<h1 align="center">TOMOSIA VPN</h1>

<p align="center">
  <b>Tiếng Việt</b> · <a href="README.en.md">English</a> · <a href="README.ja.md">日本語</a>
</p>

<p align="center">
  VPN L2TP/IPsec cho macOS, chạy trên thanh Menu Bar — kết nối ổn định, báo lỗi rõ ràng.
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-v0.6.1-blue.svg" alt="Version 0.6.1" />
  <img src="https://img.shields.io/badge/macOS-12.0+-black.svg" alt="macOS 12+" />
  <img src="https://img.shields.io/badge/arch-Apple%20Silicon%20%7C%20Intel-success.svg" alt="Architecture" />
</p>

---

## Giới thiệu

VPN L2TP có sẵn của macOS (*System Settings → VPN*) hay chập chờn trên Wi-Fi văn phòng hoặc Wi-Fi công cộng có tường lửa, và khi lỗi chỉ báo thông điệp chung chung.

TOMOSIA VPN thay thế nó:

- Bật/tắt VPN bằng một công tắc trên Menu Bar, quản lý nhiều cấu hình.
- Tự kết nối lại khi rớt mạng, báo cho bạn biết khi mất và khi có lại kết nối.
- Báo lỗi cụ thể: sai mật khẩu, sai shared secret, mạng chặn VPN…
- Mật khẩu và shared secret lưu trong macOS Keychain, không nằm trong file cấu hình.

Yêu cầu: macOS 12 trở lên, Mac Apple Silicon hoặc Intel.

---

## Cài đặt

### Cách 1 — File .dmg (khuyến nghị)

1. Tải [`TOMOSIA-VPN.dmg`](https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download/TOMOSIA-VPN.dmg) (luôn là bản mới nhất).
2. Mở file, kéo **TOMOSIA VPN** vào thư mục **Applications**.
3. Mở app. Lần đầu:
   - Nếu macOS báo không xác minh được nhà phát triển: chuột phải vào app → **Open**.
   - macOS hỏi mật khẩu quản trị một lần để cài phần lõi kết nối (`/usr/local/bin/vpn`).
   - macOS hỏi quyền gửi thông báo: chọn **Allow** để được báo khi VPN rớt.

### Cách 2 — Một dòng lệnh trong Terminal

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install.sh | bash
```

Script tự nhận chip Apple Silicon hoặc Intel, cài cả app và phần lõi, rồi mở app.

---

## Bắt đầu sử dụng

Xin quản trị mạng 4 thông tin: **địa chỉ server**, **shared secret (PSK)**, **tên tài khoản** và **mật khẩu**.

1. Click biểu tượng TOMOSIA VPN trên Menu Bar.
2. Bấm **+** cạnh **Configurations**.
3. Nhập:
   - **Display name**: tên gợi nhớ, ví dụ `Công ty`.
   - **Server address**: địa chỉ IP hoặc tên miền VPN.
   - **Account name** và **Password**: tài khoản đăng nhập VPN.
   - **Shared secret**: khóa chia sẻ IPsec.
   - **Send all traffic over VPN**: bật để mọi truy cập internet đi qua VPN; tắt để chỉ mạng công ty đi qua VPN.
4. Bấm **Create**, rồi bật công tắc của cấu hình để kết nối.

Sửa hoặc xóa cấu hình: bấm **…** trên dòng cấu hình, hoặc chuột phải. Cần tắt VPN trước khi sửa/xóa.

### Biểu tượng trên Menu Bar

| Biểu tượng | Ý nghĩa |
|---|---|
| Khiên đặc | Đã kết nối |
| Khiên viền mờ | Đang kết nối |
| Khiên viền | Chưa kết nối |
| Khiên viền có dấu `!` | Có lỗi: kết nối thất bại, hoặc VPN rớt và đang chờ kết nối lại |

Khi VPN rớt hoặc kết nối lại được, app gửi thông báo của macOS. Nếu không thấy thông báo: mở app → biểu tượng bánh răng (**Settings**) → **Notifications** → **Open Settings…** và bật thông báo cho TOMOSIA VPN.

### Cài đặt (Settings)

Biểu tượng bánh răng ở góc dưới popover. Thay đổi áp dụng từ lần kết nối sau.

- **MTU**: để `1280` (mặc định). Chọn `1400` chỉ khi quản trị mạng yêu cầu.
- **Kill switch**: khi VPN "Send all traffic" bị rớt, chặn internet cho tới khi VPN kết nối lại, để dữ liệu không đi ra ngoài VPN.
- **Verbose log**: ghi log chi tiết để gửi cho quản trị mạng khi cần điều tra lỗi.
- **Notifications**: trạng thái quyền gửi thông báo.

---

## Xử lý sự cố

Thẻ báo lỗi trong popover cho biết nguyên nhân và có nút **Try Again** / **Update Password**.

| Thông báo | Nguyên nhân | Cách xử lý |
|---|---|---|
| Authentication Failed | Sai tên tài khoản hoặc mật khẩu | Bấm **Update Password**, nhập lại |
| IKE Handshake Failed | Sai shared secret | **…** → **Edit…**, nhập lại shared secret |
| Server Not Responding | Mạng đang dùng chặn VPN (UDP 500/4500) hoặc server không hoạt động | Thử mạng khác (ví dụ 4G), hoặc hỏi quản trị mạng |
| Waiting for Network | Mac đang mất mạng | VPN tự kết nối lại khi có mạng |
| VPN Stopped Unexpectedly | Tiến trình VPN bị dừng | Bật lại công tắc |

Vẫn không kết nối được? Mở Terminal và chạy:

```bash
vpn diagnose    # kiểm tra đường mạng, DNS và cổng UDP 500/4500
vpn repair      # khôi phục route/DNS nếu mạng bị lỗi sau khi VPN rớt
```

Gửi kết quả `vpn diagnose` và `vpn logs` cho quản trị mạng.

---

## Cập nhật và gỡ cài đặt

**Cập nhật:** tải lại file `.dmg` mới nhất và kéo đè vào **Applications**, hoặc chạy lại lệnh cài đặt một dòng ở trên. Phiên bản hiện tại hiển thị ở góc dưới popover (ví dụ `v0.6.1`).

**Gỡ cài đặt** (xóa app, phần lõi, cấu hình và mật khẩu đã lưu trong Keychain):

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/uninstall.sh | bash
```

---

## Dùng bằng dòng lệnh (nâng cao)

Mọi thao tác trong app đều làm được bằng lệnh `vpn`:

```bash
vpn profile add work --server vpn.example.com   # hỏi shared secret
vpn account add work nguyenvana --default       # hỏi mật khẩu
vpn connect
vpn status
vpn disconnect
```

| Lệnh | Tác dụng |
|---|---|
| `vpn connect [--profile <tên>]` / `vpn disconnect` | Kết nối / ngắt kết nối |
| `vpn status` | Trạng thái và IP |
| `vpn profile list` | Danh sách cấu hình (`*` = đang chọn) |
| `vpn profile add <tên> --server <host>` | Thêm cấu hình |
| `vpn profile edit <tên> [--server host] [--user tên] [--full-tunnel=bool] [--set-psk]` | Sửa cấu hình, giữ mật khẩu đã lưu |
| `vpn profile rename <tên> [tên hiển thị]` | Đổi tên hiển thị |
| `vpn profile remove <tên>` | Xóa cấu hình và secret trong Keychain |
| `vpn account add <cấu hình> <user>` | Thêm tài khoản |
| `vpn mtu [1280\|1400]`, `vpn killswitch [on\|off]`, `vpn verbose [on\|off]` | Cài đặt chung |
| `vpn diagnose`, `vpn logs -f`, `vpn repair` | Chẩn đoán và khôi phục mạng |
| `vpn update` | Cập nhật phần lõi (kiểm tra chữ ký số) |
| `vpn version` | Phiên bản |

---

## Đóng góp

Build từ mã nguồn, chạy test, quy trình release: xem [CONTRIBUTING.md](CONTRIBUTING.md).
