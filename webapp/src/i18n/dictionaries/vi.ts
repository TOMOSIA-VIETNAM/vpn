import type { Dictionary } from "../dictionary";

const vi: Dictionary = {
  meta: {
    title: "TOMOSIA VPN — VPN công ty tự kết nối lại khi rớt mạng",
    description:
      "Ứng dụng trên thanh menu cho nhân viên TOMOSIA dùng Mac. Bật VPN công ty chỉ bằng một công tắc; khi Wi-Fi làm rớt kết nối, app tự kết nối lại và nói rõ lỗi bằng lời dễ hiểu.",
  },
  header: {
    homeLabel: "Trang chủ TOMOSIA VPN",
    languageLabel: "Ngôn ngữ",
    download: "Tải về",
  },
  hero: {
    titleLead: "Wi-Fi chập chờn?",
    titleStrong: "VPN vẫn tự kết nối lại.",
    lead: "TOMOSIA VPN gói VPN công ty vào một công tắc trên thanh menu của Mac. Rớt kết nối thì app tự nối lại. Có lỗi thì app nói rõ cần sửa gì.",
    download: "Tải cho Mac",
    requirements: "macOS {version}+ · Apple Silicon & Intel",
    video: {
      pause: "Tạm dừng video",
      play: "Phát video",
      soundOn: "Xem có tiếng",
      soundOff: "Tắt tiếng",
      turnOnSound: "Bật tiếng",
      playWithSound: "Phát video có tiếng",
      about: "Video {seconds} giây · có thuyết minh",
    },
  },
  stats: {
    title: "TOMOSIA VPN qua vài con số",
    switchLabel: "công tắc trên thanh menu để bật, tắt VPN",
    errorsLabel: "lỗi thường gặp được gọi đúng tên, kèm cách xử lý",
    reconnectLabel: "lần bấm để kết nối lại khi Wi-Fi rớt",
    macValue: "Apple Silicon + Intel",
    macLabel: "Mọi máy Mac chạy macOS {version} trở lên",
  },
  problem: {
    eyebrow: "Vì sao cần app này",
    title: "VPN có sẵn của macOS hay rớt mà chẳng nói lý do.",
    body: "VPN công ty dùng một kiểu kết nối đời cũ mà VPN có sẵn của macOS xử lý không tốt. Ở Wi-Fi văn phòng hay quán cà phê, kết nối hay bị ngắt, rồi chỉ hiện đúng một thông báo mơ hồ, dù bạn nhập sai mật khẩu, mạng chặn VPN hay đang mất mạng.",
    detail: "Về kỹ thuật: VPN công ty là L2TP/IPsec dùng shared secret.",
    toggleLabel: "So sánh thông báo lỗi",
    builtInLabel: "VPN có sẵn",
    appLabel: "TOMOSIA VPN",
    builtInCaption: "Bốn lỗi khác nhau, cùng một thông báo mơ hồ.",
    appCaption: "Mỗi lỗi một tên riêng, kèm nút để xử lý.",
    causes: ["Sai mật khẩu", "Sai shared secret", "Mạng chặn VPN", "Mất internet"],
  },
  story: {
    eyebrow: "Cách hoạt động",
    title: "Bạn cứ làm việc, kết nối để app lo.",
    steps: [
      {
        title: "Bật lên",
        body: "Bấm biểu tượng chiếc khiên trên thanh menu, gạt công tắc Office. Vậy là xong cho cả ngày.",
      },
      {
        title: "Wi-Fi rớt",
        body: "Wi-Fi văn phòng chập chờn, hoặc bạn ra quán cà phê ngồi. Một thông báo cho bạn biết kết nối vừa mất.",
      },
      {
        title: "Tự kết nối lại",
        body: "TOMOSIA VPN thử lại liên tục cho đến khi có kết nối, rồi báo cho bạn. Bạn không cần bấm gì.",
      },
      {
        title: "Nói rõ lỗi ở đâu",
        body: "Nếu không kết nối được, app nói rõ lý do, ví dụ sai mật khẩu, và đặt sẵn nút để sửa.",
      },
    ],
  },
  features: {
    eyebrow: "Có gì bên trong",
    title: "Làm ra cho Wi-Fi hay chập chờn.",
    lead: "Những tiện ích nhỏ giúp bạn khỏi phải gọi IT.",
    items: {
      menuBar: {
        title: "Một công tắc cho mọi kết nối",
        body: "Lưu Office, phòng lab hay văn phòng khách hàng trong một danh sách, muốn bật cái nào chỉ cần gạt công tắc.",
        detail: "Khi đang kết nối, menu hiện IP công khai mà bạn đang dùng.",
      },
      reconnect: {
        title: "Rớt là tự nối lại",
        body: "Sau khi rớt, app thử lại liên tục và báo cho bạn khi đã kết nối lại.",
        detail: "Thông báo của macOS lúc mất kết nối và lúc có lại.",
      },
      errors: {
        title: "Lỗi được nói bằng lời dễ hiểu",
        body: "Sai mật khẩu, sai shared secret, mạng chặn hay mất internet: mỗi lỗi một thông báo, một cách xử lý.",
        detail: "Các nút như [[Update Password]] và [[Try Again]] đưa bạn thẳng tới bước tiếp theo.",
      },
      network: {
        title: "Mất mạng thì kiên nhẫn chờ",
        body: "Không có Wi-Fi? App chờ và kết nối lại ngay khi Mac có mạng trở lại.",
        detail: "Hiện [[Waiting for Network…]] thay vì báo lỗi.",
      },
      killSwitch: {
        title: "Không để lọt dữ liệu",
        body: "Nếu VPN rớt, internet tạm dừng cho đến khi kết nối lại, nên không có gì đi ra ngoài mà thiếu bảo vệ.",
        detail: "Kill switch, dành cho cấu hình gửi toàn bộ lưu lượng qua VPN.",
      },
      keychain: {
        title: "Mật khẩu được cất kỹ",
        body: "Mật khẩu và shared secret nằm trong Keychain của macOS, không bao giờ lưu ra file.",
        detail: "Mỗi bí mật là một mục riêng trong Keychain, được xoá khi gỡ app.",
      },
    },
  },
  screens: {
    eyebrow: "Thiết lập",
    title: "Thiết lập một lần, với thông tin IT đưa bạn.",
    newConfigurationAlt:
      "Biểu mẫu New Configuration: tên hiển thị, địa chỉ server, tài khoản, mật khẩu, shared secret và Send all traffic over VPN.",
    newConfigurationCaption: "Nhập server, tài khoản và shared secret do quản trị mạng cung cấp. Thế là xong.",
    settingsAlt: "Cửa sổ Settings ở giao diện Tối, gồm các tuỳ chọn kết nối, Kill switch và Notifications.",
    settingsCaption: "Chọn có tạm dừng internet khi VPN rớt hay không, và muốn nhận những thông báo nào.",
  },
  install: {
    eyebrow: "Cài đặt",
    title: "Ba bước là dùng được.",
    steps: [
      { title: "Tải về", body: "Tải file {dmg}." },
      { title: "Kéo vào Applications", body: "Mở file, kéo **TOMOSIA VPN** vào thư mục **Applications**." },
      { title: "Mở app", body: "Mở app lên. Nếu macOS chặn, chuột phải vào app rồi chọn **Open**." },
    ],
    uninstall: {
      summary: "Khi cần gỡ cài đặt",
      body: "Chạy lệnh này trong Terminal bằng tài khoản thường của bạn. Lệnh sẽ gỡ app, công cụ dòng lệnh đi kèm, phần cài đặt và các mật khẩu app đã lưu trong Keychain.",
      copy: "Sao chép",
      copied: "Đã sao chép",
    },
  },
  cta: {
    title: "Kết nối ổn định, dù bạn làm việc ở đâu.",
    body: "Tải về, gạt công tắc, rồi yên tâm quay lại với công việc.",
  },
  footer: {
    tagline: "VPN công ty, chỉ cách bạn một công tắc.",
    github: "GitHub",
    releases: "Tất cả phiên bản",
    developers: "Dành cho lập trình viên",
  },
};

export default vi;
