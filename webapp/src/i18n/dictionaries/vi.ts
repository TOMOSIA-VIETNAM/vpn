import type { Dictionary } from "../dictionary";

const vi: Dictionary = {
  meta: {
    title: "TOMOSIA VPN — VPN cho Mac luôn giữ kết nối",
    description:
      "Ứng dụng VPN L2TP/IPsec trên thanh menu macOS dành cho nhân viên TOMOSIA. Tự kết nối lại khi rớt mạng ở Wi-Fi văn phòng hay nơi công cộng và báo rõ lỗi nằm ở đâu.",
  },
  header: {
    homeLabel: "Trang chủ TOMOSIA VPN",
    languageLabel: "Ngôn ngữ",
    github: "GitHub",
  },
  hero: {
    title: "VPN luôn giữ kết nối.",
    lead: "TOMOSIA VPN thay cho VPN L2TP có sẵn của macOS, chỉ với một công tắc trên thanh menu. Khi Wi-Fi văn phòng hay nơi công cộng làm rớt kết nối, app tự kết nối lại. Khi có lỗi, app nói rõ cần sửa gì.",
    download: "Tải cho Mac",
    requirements: "macOS {version}+ · Apple Silicon & Intel",
    popoverAlt:
      "Menu TOMOSIA VPN: đang kết nối cấu hình Office, IP công khai 203.0.113.24, cấu hình thứ hai Research Lab đang tắt.",
    video: {
      pause: "Tạm dừng video",
      play: "Phát video",
      soundOn: "Xem có tiếng",
      soundOff: "Tắt tiếng",
    },
  },
  problem: {
    title: "VPN có sẵn của macOS ngắt mà không nói lý do.",
    context:
      "VPN công ty chạy L2TP/IPsec với shared secret, một giao thức đã cũ. VPN có sẵn của macOS chạy giao thức này không ổn định, còn nhiều app VPN khác lại cần chứng chỉ hoặc giao thức riêng.",
    body: "Trong System Settings → VPN, kết nối hay rớt khi đi qua tường lửa của Wi-Fi văn phòng và nơi công cộng, rồi hiện cùng một thông báo cho mọi lỗi: sai mật khẩu, cổng bị chặn hay mất mạng.",
    builtInLabel: "VPN có sẵn của macOS",
    builtInCaption: "Một thông báo cho mọi loại lỗi.",
    appLabel: "TOMOSIA VPN",
    appCaption: "Mỗi nguyên nhân một thông báo riêng, nút bấm chính là bước cần làm tiếp.",
  },
  features: {
    title: "Dành cho mạng hay chập chờn.",
    items: {
      menuBar: {
        title: "Một công tắc trên thanh menu",
        body: "Bật, tắt VPN ngay trên thanh menu. Lưu nhiều cấu hình, ví dụ Office và phòng lab, rồi chuyển qua lại giữa chúng.",
      },
      reconnect: {
        title: "Tự kết nối lại",
        body: "Khi rớt kết nối, app thử lại liên tục đến khi đường hầm hoạt động trở lại. Thông báo của macOS cho bạn biết lúc mất kết nối và lúc kết nối lại.",
      },
      errors: {
        title: "Lỗi nói rõ cần làm gì",
        body: "Sai mật khẩu, sai shared secret, mạng chặn UDP 500/4500 hay không có mạng: mỗi lỗi một thông báo riêng, kèm nút [[Try Again]] hoặc [[Update Password]].",
      },
      killSwitch: {
        title: "Kill switch",
        body: "Nếu VPN full-tunnel bị rớt, lưu lượng internet bị chặn đến khi kết nối lại, nên không có gì lọt ra ngoài đường hầm.",
      },
      publicIp: {
        title: "Thấy ngay IP công khai",
        body: "Khi đang kết nối, menu hiện IP công khai mà lưu lượng của bạn đi ra internet.",
      },
      keychain: {
        title: "Bí mật nằm trong Keychain",
        body: "Mật khẩu và shared secret được lưu trong Keychain của macOS, không bao giờ nằm trong file văn bản thường.",
      },
    },
  },
  screens: {
    title: "Thiết lập chỉ cần một biểu mẫu.",
    settingsAlt: "Cửa sổ Settings gồm MTU, Verbose log, Kill switch và Notifications.",
    settingsCaption:
      "Settings: MTU, Verbose log, Kill switch và Notifications. Thay đổi áp dụng từ lần kết nối sau.",
    newConfigurationAlt:
      "Biểu mẫu New Configuration gồm tên hiển thị, địa chỉ server, tài khoản, mật khẩu, shared secret và Send all traffic over VPN.",
    newConfigurationCaption:
      "Cấu hình mới chỉ cần server, tài khoản và shared secret do quản trị mạng cung cấp.",
  },
  install: {
    title: "Cài đặt",
    steps: [
      "Tải {dmg}.",
      "Mở file, kéo **TOMOSIA VPN** vào **Applications**.",
      "Mở app (nếu macOS chặn: chuột phải vào app → **Open**).",
    ],
    requirement: "Yêu cầu macOS {version} trở lên.",
  },
  uninstall: {
    title: "Gỡ cài đặt",
    body: "Chạy lệnh sau trong Terminal bằng tài khoản thường của bạn. Lệnh gỡ app, công cụ dòng lệnh vpn, cấu hình và các bí mật đã lưu trong Keychain.",
    copy: "Sao chép",
    copied: "Đã sao chép",
  },
  footer: {
    developers: "Dành cho người phát triển",
    source: "Mã nguồn trên GitHub",
  },
};

export default vi;
