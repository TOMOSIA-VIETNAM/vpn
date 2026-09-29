import type { Dictionary } from "../dictionary";

const ja: Dictionary = {
  meta: {
    title: "TOMOSIA VPN — 切れても自動でつながる Mac 用 VPN",
    description:
      "TOMOSIA 社員向けの macOS メニューバー L2TP/IPsec VPN クライアント。オフィスや公衆 Wi-Fi で切断されても自動で再接続し、原因をはっきり伝えます。",
  },
  header: {
    homeLabel: "TOMOSIA VPN ホーム",
    languageLabel: "言語",
    github: "GitHub",
  },
  hero: {
    title: "つながり続ける VPN。",
    lead: "TOMOSIA VPN は、macOS 標準の L2TP VPN をメニューバーのスイッチひとつで置き換えます。オフィスや公衆 Wi-Fi で接続が切れても自動で再接続し、問題があれば何を直せばよいかを表示します。",
    download: "Mac 版をダウンロード",
    requirements: "macOS {version} 以降 · Apple Silicon & Intel",
    popoverAlt:
      "TOMOSIA VPN のメニュー：Office に接続中、パブリック IP 203.0.113.24、2 つ目の構成 Research Lab はオフ。",
  },
  problem: {
    title: "標準の VPN は、理由を告げずに切れる。",
    body: "システム設定 → VPN の L2TP クライアントは、オフィスや公衆 Wi-Fi のファイアウォール越しによく切断されます。しかもパスワードの誤り、ポートのブロック、ネットワークの切断、どれでも同じメッセージしか出ません。",
    builtInLabel: "macOS 標準の VPN",
    builtInCaption: "どの失敗でも同じメッセージ。",
    appLabel: "TOMOSIA VPN",
    appCaption: "原因ごとに専用のアラート。ボタンが次にやることです。",
  },
  features: {
    title: "不安定なネットワークのために。",
    items: {
      menuBar: {
        title: "メニューバーにスイッチひとつ",
        body: "メニューバーから VPN をオン・オフ。Office やラボなど複数の構成を保存して、切り替えられます。",
      },
      reconnect: {
        title: "自動で再接続",
        body: "切断されると、トンネルが戻るまで再試行を続けます。切断時と復帰時には macOS の通知でお知らせします。",
      },
      errors: {
        title: "対処がわかるエラー",
        body: "パスワードの誤り、共有シークレットの誤り、UDP 500/4500 をブロックするネットワーク、オフライン。それぞれに専用のメッセージと [[Try Again]] / [[Update Password]] ボタンがあります。",
      },
      killSwitch: {
        title: "Kill switch",
        body: "フルトンネルの VPN が切れたら、再接続するまでインターネット通信を遮断します。トンネルの外に通信が漏れません。",
      },
      publicIp: {
        title: "パブリック IP をすぐ確認",
        body: "接続中は、通信の出口となるパブリック IP をメニューに表示します。",
      },
      keychain: {
        title: "秘密情報はキーチェーンに",
        body: "パスワードと共有シークレットは macOS のキーチェーンに保存され、平文のファイルには残りません。",
      },
    },
  },
  screens: {
    title: "設定はフォームひとつ。",
    settingsAlt: "MTU、Verbose log、Kill switch、Notifications を含む Settings ウィンドウ。",
    settingsCaption:
      "Settings：MTU、Verbose log、Kill switch、Notifications。変更は次回の接続から反映されます。",
    newConfigurationAlt:
      "表示名、サーバアドレス、アカウント名、パスワード、共有シークレット、Send all traffic over VPN を含む New Configuration フォーム。",
    newConfigurationCaption:
      "新しい構成に必要なのは、ネットワーク管理者から受け取るサーバ、アカウント、共有シークレットだけです。",
  },
  install: {
    title: "インストール",
    steps: [
      "{dmg} をダウンロードします。",
      "開いて **TOMOSIA VPN** を **アプリケーション** へドラッグします。",
      "アプリを開きます（macOS にブロックされた場合：アプリを右クリック → **開く**）。",
    ],
    requirement: "動作環境：macOS {version} 以降。",
  },
  uninstall: {
    title: "アンインストール",
    body: "ターミナルで、通常のユーザーとして次のコマンドを実行します。アプリ、vpn コマンドラインツール、設定、キーチェーンに保存した秘密情報を削除します。",
    copy: "コピー",
    copied: "コピーしました",
  },
  footer: {
    developers: "開発者向け",
    source: "GitHub のソースコード",
  },
};

export default ja;
