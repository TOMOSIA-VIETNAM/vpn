<p align="center">
  <img src="assets/logo.png" width="96" height="96" alt="TMS VPN logo" />
</p>

<h1 align="center">TMS VPN</h1>

VPN client L2TP/IPsec thuần macOS — không phụ thuộc strongSwan, xl2tpd, pppd, Docker hay WireGuard.

- **App TMS VPN** (menu bar): dùng hằng ngày — thêm hồ sơ, bật/tắt kết nối, xem IP.
- **CLI `vpn`**: engine thực hiện kết nối (IKE, ESP, L2TP, PPP, route, DNS). App gọi CLI này; dùng trực tiếp từ terminal cũng được.

## Vì sao có tool này

**Hiện trạng:** kết nối tới server công ty bằng VPN có sẵn của macOS (System Settings → VPN → L2TP over IPsec) không ổn định: lúc được lúc không, tuỳ mạng đang dùng. Khi lỗi, macOS chỉ báo chung chung, không biết hỏng ở bước nào để tự sửa hay báo admin.

TMS VPN tự cài đặt toàn bộ giao thức (IKE, ESP, L2TP, PPP) thay vì dùng client có sẵn, nên xử lý được những tình huống đã gặp khi dùng thật:

| | TMS VPN làm gì |
|---|---|
| **Mạng khó** | Có mạng văn phòng mà server không trả lời IKE gửi từ cổng nguồn UDP/500 (thường do router "IPsec passthrough"). Client thử cổng 500 trước, không có phản hồi thì tự chuyển sang cổng khác. NAT-T qua UDP/4500 và MTU 1280 (`vpn mtu`) cho hotspot/PPPoE. |
| **Giữ kết nối lâu** | Trả lời keepalive của server (IKE DPD, PPP LCP Echo) để server không tưởng máy đã mất rồi cắt phiên. Tự làm mới khoá mã hoá (rekey) trong lúc kết nối, không rớt phiên. Mất kết nối thì tự nối lại; tuỳ chọn killswitch chặn internet trong lúc đó (full tunnel). |
| **Biết hỏng ở đâu** | Lỗi ghi rõ bước và nguyên nhân (xem [Sự cố](#sự-cố)). `vpn diagnose` gửi gói IKE thật tới server để kiểm tra UDP 500/4500. `vpn logs` có log từng bước giao thức. |
| **Dễ dùng** | Nhiều profile/account, bật/tắt từ menu bar hoặc terminal. Secret nằm trong Keychain. Không cần cài thêm strongSwan, xl2tpd hay Docker. |

## Yêu cầu

- macOS 12+ (Apple Silicon hoặc Intel), quyền admin để cài (`sudo` một lần).
- Server VPN: L2TP/IPsec, IKEv1 Main Mode + pre-shared key (PSK), đăng nhập PPP bằng MS-CHAPv2, chỉ IPv4 (NAT-T qua UDP 4500). Không hỗ trợ IKEv2, chứng chỉ, PAP hay CHAP-MD5.
- Xin admin 4 thông tin: địa chỉ server, PSK, username, password.

## Cài đặt

**Cài đặt tự động (tự nhận diện chip Apple Silicon M1/M2/M3... hoặc Mac Intel 2017+):**

```bash
curl -fsSL https://raw.githubusercontent.com/tms-ninhle/vpn/main/install.sh | bash
```

Hoặc cài đặt chỉ định theo từng kiến trúc:

```bash
curl -fsSL https://raw.githubusercontent.com/tms-ninhle/vpn/main/install-arm64.sh | bash   # Apple Silicon (M1/M2/M3...)
curl -fsSL https://raw.githubusercontent.com/tms-ninhle/vpn/main/install-intel.sh | bash   # Mac Intel
```

Installer cài CLI vào `/usr/local/bin/vpn` (setuid-root, xem [Bảo mật](#bảo-mật)) và app vào `/Applications/TMS VPN.app`, sau khi đối chiếu `SHA256SUMS` của release.

```bash
vpn version    # xác nhận cài xong
```

## Kết nối lần đầu

**Qua app (khuyến nghị):** bấm biểu tượng TMS VPN trên menu bar → **Add** → nhập Display name, Server address, Account name, Password, Shared secret (PSK) → **Create** → bật công tắc để kết nối. Menu **Edit**/**Delete** cạnh công tắc để sửa hoặc xoá. PSK và mật khẩu lưu trong Keychain của macOS, không nằm trong file cấu hình.

**Qua terminal:**

```bash
vpn profile add work --server vpn.example.com     # hỏi PSK
vpn account add work nguyenvana --default         # hỏi password
vpn connect                                       # chạy nền, trả lại terminal khi biết kết quả
```

> Không truyền `--psk`/`--password` trên dòng lệnh — chúng lưu lại trong shell history và lộ qua `ps` cho user khác trên máy. Để CLI hỏi trực tiếp.

**Xác nhận đã qua VPN:**

```bash
vpn status                     # Phase: CONNECTED, kèm tunnel và IP được cấp
curl -4 https://ifconfig.co    # phải trả về IP của VPN
```

## Dùng hằng ngày

```bash
vpn connect [--profile <tên>] [--account <user>] [--force]   # --force: ép nối lại dù đã kết nối
vpn disconnect
vpn status
```

Khoá mã hoá tự làm mới định kỳ trong lúc kết nối, không làm rớt phiên và không cần đăng nhập lại. Mất kết nối thì client tự nối lại — `vpn status` hiện `Reconnecting` trong lúc đó.

## Profile và account

- **Profile** = một server (địa chỉ, PSK, chế độ tunnel). Mỗi profile có thể có nhiều **account**, một trong số đó là default.
- **Profile active** là profile `vpn connect` dùng khi không truyền `--profile`. Xoá profile active thì profile khác (theo thứ tự tên) tự thành active.

```bash
vpn profile list                                                # * = profile/account đang active
vpn profile add <tên> --server <host> [--server-id id] [--mtu n] [--full-tunnel=false]
vpn profile rename <tên> [display name]                         # đổi tên hiển thị, không đổi secret đã lưu
vpn profile remove <tên>
vpn account add <profile> <user> [--default]
```

| Flag của `profile add` | Ý nghĩa |
|---|---|
| `--server-id <id>` | ID server phải tự khai trong IKE; để trống thì chấp nhận mọi ID |
| `--mtu <n>` | MTU riêng của profile (mặc định 1400) — `vpn mtu` chung (bên dưới) được ưu tiên hơn |
| `--full-tunnel=false` | Split tunnel: chỉ traffic tới server VPN và DNS server được cấp đi qua tunnel. Mặc định full tunnel: toàn bộ IPv4 qua VPN, IPv6 bị chặn |

Ở full tunnel, nếu server không cấp DNS, `vpn connect`/`vpn status` sẽ cảnh báo: DNS vẫn đi qua resolver của mạng hiện tại thay vì qua VPN.

## Cài đặt chung

```bash
vpn mtu [1280|1400]      # MTU cho mọi profile — 1280 nếu mạng hay đứng khi tải lớn (hotspot, PPPoE)
vpn verbose [on|off]     # log chi tiết giao thức để chẩn đoán, mặc định off
vpn killswitch [on|off]  # chặn internet khi VPN full-tunnel rớt và đang tự nối lại, mặc định off
```

Đổi có hiệu lực ở lần `connect` tiếp theo. Menu bar app có các mục tương ứng trong **Settings** (biểu tượng bánh răng).

## Sự cố

```bash
vpn diagnose      # kiểm tra mạng, DNS, UDP 500/4500, MTU — không thay đổi gì trên máy
vpn logs -f       # xem log (/var/log/vpn.log)
vpn repair        # dọn route/DNS nếu vpn bị crash hoặc bị kill giữa chừng
```

Lỗi hiện ra dạng `STAGE: mô tả: chi tiết` — đọc phần chi tiết để biết nguyên nhân cụ thể.

| Mã / nội dung | Nguyên nhân thường gặp | Cách xử lý |
|---|---|---|
| `IKE_TIMEOUT` + `IKE_AUTH_FAILED` / `HASH_R mismatch` | Sai PSK | `vpn profile add <tên> --server <host>` để nhập lại |
| `IKE_TIMEOUT` + `no response` | Server không trả lời, mạng chặn UDP 500/4500 | `vpn diagnose`; thử mạng khác |
| `IKE_TIMEOUT` + `IKE_PROPOSAL_MISMATCH` | Server không chấp nhận thuật toán nào client đề xuất | Báo admin kèm `vpn logs` |
| `PPP_AUTH_FAILURE` + `CHAP authentication rejected` | Sai username/password | `vpn account add <profile> <user>` để nhập lại |
| `already logged in` | Phiên cũ trên server chưa hết | Đợi vài giây rồi `vpn connect` lại |
| `authenticator response mismatch` | Server không chứng minh được biết mật khẩu — có thể bị giả mạo | Đừng nhập lại mật khẩu; đổi mạng và báo admin |
| `L2TP_TIMEOUT`, `LCP_FAILED`, `IPCP_FAILURE` | Lỗi ở tầng L2TP/PPP | Báo admin kèm `vpn logs` |
| `DNS_FAILURE` + `resolve VPN server` | Không phân giải được tên server | Kiểm tra mạng, hoặc dùng IP thay tên |
| `ROUTE_FAILURE`, `TUN_FAILURE` | Lỗi cấu hình mạng trên máy | `vpn repair` rồi thử lại |
| `installed by a different user` | Chỉ user đã cài mới chạy được `vpn` | Dùng đúng user đó, hoặc cài lại |
| `no account selected` / `no PSK stored` | Profile thiếu account hoặc secret | Làm theo lệnh gợi ý trong thông báo lỗi |

**File nằm ở đâu:**

| Đường dẫn | Nội dung |
|---|---|
| `~/.config/vpn/config.json` | Profile, account, tuỳ chọn (không chứa secret) |
| `~/.config/vpn/session.json` | ID phiên L2TP gần nhất — giúp tránh lỗi "already logged in" khi tự nối lại sau mất kết nối đột ngột |
| Keychain: `vpn.psk.<profile>`, `vpn.pwd.<profile>.<account>` | PSK và password |
| `/var/log/vpn.log`, `/var/log/vpn.log.1` | Log (tự xoay vòng) |
| `/var/run/vpn/state.json` | Trạng thái kết nối hiện tại |
| `/etc/vpn-owner-uid` | UID của user đã cài |

## Cập nhật / gỡ cài đặt

```bash
vpn update [--force]    # verify chữ ký + SHA-256, chỉ cài nếu mới hơn bản đang chạy (--force: cài lại/hạ cấp)
vpn uninstall [-y]      # gỡ CLI, log, state, mọi profile/account kèm secret trong Keychain (giữ lại app)
```

Muốn cập nhật cả app, chạy lại lệnh cài ở trên. Muốn gỡ cả app, dùng `uninstall.sh`:

```bash
curl -fsSL https://raw.githubusercontent.com/tms-ninhle/vpn/main/uninstall.sh | bash
```

## Bảo mật

CLI cài setuid-root, nhưng tự hạ quyền về user thường ngay khi khởi động và chỉ tạm nâng lại đúng lúc cần (mở utun, bind UDP/500, đổi route/DNS) — không giữ quyền root suốt phiên kết nối. Chỉ user đã chạy installer mới gọi được `vpn`; user khác trên máy bị từ chối ngay.

- **Traffic:** full tunnel đưa toàn bộ IPv4 qua VPN và chặn IPv6; ESP dùng đúng thuật toán đã negotiate với server (AES hoặc 3DES, HMAC-SHA256 hoặc SHA1).
- **Xác thực server:** kiểm tra `S=` của MS-CHAPv2, nên chỉ có PSK (thường dùng chung) không giả được server.
- **Secret:** lưu trong Keychain, truyền cho `security` qua stdin nên không lộ qua `ps`. Log không ghi payload đã giải mã.
- **Cập nhật:** `vpn update` verify chữ ký ed25519 bằng public key nhúng sẵn trong binary. Installer chỉ kiểm `SHA256SUMS`, chống được file tải hỏng nhưng không chống được repo bị chiếm.

## Dành cho developer

| Đường dẫn | Nội dung |
|---|---|
| `cmd/vpn` | Entry point của CLI |
| `internal/ike`, `ipsec`, `l2tp`, `ppp` | Các tầng giao thức: IKEv1, ESP, L2TP, PPP/MS-CHAPv2 |
| `internal/engine` | Điều phối kết nối: data plane, rekey, tự nối lại, kill switch |
| `internal/routing`, `dnsmgr`, `tun`, `netwatch` | Route, DNS, thiết bị utun và theo dõi sự kiện mạng của macOS |
| `internal/cli`, `config`, `keychain`, `state` | Lệnh CLI, cấu hình, Keychain, file trạng thái |
| `internal/diagnostics` | Backend của `vpn diagnose` |
| `internal/privilege`, `sysbin`, `release`, `vpnlog` | setuid, đường dẫn lệnh hệ thống, ký release, log |
| `cmd/releasesign` | Công cụ ký release (dùng trong CI) |
| `main.swift`, `build.sh` | App menu bar (Swift) và script build |
| `src/`, `index.html`, `package.json` | Prototype giao diện (React/Vite, dữ liệu giả) — không phải app thật |

**Yêu cầu:** Go theo `go.mod` (hiện 1.27) — `brew install go` (nếu `go version` vẫn ra bản cũ, gỡ bản `.pkg` cũ ở `/usr/local/go`); Xcode 26/Swift 6 để build app (Swift 5.9 không build được `main.swift`); [bun](https://bun.sh) nếu muốn chạy prototype.

```bash
git clone https://github.com/tms-ninhle/vpn.git && cd vpn

# CLI — cài đúng chỗ và đúng quyền như installer
go build -o vpn ./cmd/vpn
sudo install -o root -g wheel -m 4755 vpn /usr/local/bin/vpn
id -u | sudo tee /etc/vpn-owner-uid >/dev/null && sudo chmod 600 /etc/vpn-owner-uid

# App
bash build.sh
ditto "build/TMS VPN.app" "/Applications/TMS VPN.app"
```

Không có Xcode 26? Tải app đã build sẵn cho một PR thay vì `bash build.sh` (tab **Checks** → workflow `test` → artifact **TMS-VPN-app**):

```bash
gh run download <run-id> --repo tms-ninhle/vpn -n TMS-VPN-app
unzip TMS-VPN.app.zip && ditto "TMS VPN.app" "/Applications/TMS VPN.app"
```

```bash
go vet ./... && go test ./...                      # CI chạy gofmt, vet và test trên macOS
go test -tags keychain_live ./internal/keychain    # ghi/đọc thật vào login Keychain (chạy tay)
vpn connect --verbose --rekey-after 90s            # test live: ép rekey mỗi 90 giây
bun install && bun run dev                          # prototype giao diện — http://localhost:3000, chỉ là mockup, không phải app thật
```

## Phát hành

Release được tạo khi push tag semver, sau khi CI pass:

```bash
git tag v0.4.4 && git push origin v0.4.4   # bản mới nhất hiện tại: v0.4.4
```

Mỗi release gồm `vpn-darwin-arm64`, `vpn-darwin-amd64`, `TMS-VPN.app.zip`, `SHA256SUMS` và `SHA256SUMS.sig` (chữ ký ed25519). `vpn update` từ chối release nếu chữ ký hoặc SHA-256 không khớp, hoặc version không mới hơn bản đang chạy (`--force` để bỏ qua).

Khoá ký nằm trong secret `RELEASE_SIGNING_KEY` của repo. Tạo cặp khoá mới:

```bash
go run ./cmd/releasesign keygen   # stdout: seed → secret; stderr: public key → release.PublicKey
```
