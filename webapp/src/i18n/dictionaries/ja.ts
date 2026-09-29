import type { Dictionary } from "../dictionary";

// Headlines spell Wi‑Fi with a non-breaking hyphen (U+2011), so a line never
// breaks between "Wi" and "Fi".
const ja: Dictionary = {
  meta: {
    title: "TOMOSIA VPN — 切れても自動でつながる社内 VPN",
    description:
      "TOMOSIA 社員向けの Mac メニューバーアプリ。スイッチひとつで社内 VPN に接続し、Wi-Fi で切れても自動で再接続。問題があれば原因をわかりやすく伝えます。",
  },
  header: {
    homeLabel: "TOMOSIA VPN ホーム",
    languageLabel: "言語",
    download: "ダウンロード",
  },
  hero: {
    titleLead: "Wi‑Fi が切れても、",
    titleStrong: "VPN はすぐに戻る。",
    lead: "TOMOSIA VPN は、社内 VPN を Mac のメニューバーのスイッチひとつにまとめます。接続が切れたら自動で再接続。問題があれば、何を直せばよいかを表示します。",
    download: "Mac 版をダウンロード",
    requirements: "macOS {version} 以降 · Apple Silicon & Intel",
    video: {
      pause: "動画を一時停止",
      play: "動画を再生",
      soundOn: "音声付きで見る",
      soundOff: "ミュート",
      turnOnSound: "音声をオン",
      playWithSound: "音声付きで再生",
    },
  },
  stats: {
    title: "TOMOSIA VPN のポイント",
    switchLabel: "メニューバーのスイッチで VPN をオン・オフ",
    errorsLabel: "よくある問題を名前で示し、対処法も表示",
    reconnectLabel: "Wi-Fi が切れた後、再接続に必要なクリック数",
    macValue: "Apple Silicon + Intel",
    macLabel: "macOS {version} 以降のすべての Mac に対応",
  },
  problem: {
    eyebrow: "このアプリがある理由",
    title: "標準の VPN は、理由も告げずに切れる。",
    body: "社内 VPN は古い接続方式を使っていて、macOS 標準の VPN ではうまく扱えません。オフィスや公衆 Wi-Fi では接続が切れやすく、パスワードの誤りでも、ネットワークによるブロックでも、オフラインでも、同じあいまいなメッセージしか出ません。",
    detail: "技術メモ：社内 VPN は共有シークレットを使う L2TP/IPsec です。",
    toggleLabel: "メッセージを比較",
    builtInLabel: "標準の VPN",
    appLabel: "TOMOSIA VPN",
    builtInCaption: "原因は 4 つ、メッセージはひとつ。",
    appCaption: "原因ごとに名前を示し、直すためのボタン付き。",
    causes: ["パスワードの誤り", "共有シークレットの誤り", "ネットワークが VPN をブロック", "インターネットに未接続"],
  },
  story: {
    eyebrow: "使い方",
    title: "仕事に集中。接続はおまかせ。",
    steps: [
      {
        title: "オンにする",
        body: "メニューバーの盾のアイコンをクリックして、Office のスイッチをオン。1 日の準備はこれだけです。",
      },
      {
        title: "Wi‑Fi が切れる",
        body: "オフィスの Wi-Fi が不安定になったり、カフェに移動したり。接続が切れたことを通知でお知らせします。",
      },
      {
        title: "自動で再接続",
        body: "接続が戻るまで TOMOSIA VPN が再試行を続け、戻ったら通知します。クリックは不要です。",
      },
      {
        title: "原因を伝える",
        body: "接続できないときは、パスワードの誤りなどの理由を表示し、直すためのボタンを用意します。",
      },
    ],
  },
  features: {
    eyebrow: "機能",
    title: "切れやすい Wi‑Fi のために。",
    lead: "IT 担当者に問い合わせる手間を減らす、小さな工夫。",
    items: {
      menuBar: {
        title: "スイッチひとつで、すべての接続を",
        body: "Office、ラボ、客先などを一覧に保存し、それぞれスイッチでオンにできます。",
        detail: "接続中は、通信に使われているパブリック IP をメニューに表示します。",
      },
      reconnect: {
        title: "切れても自動で戻る",
        body: "切断後も再試行を続け、再びつながったらお知らせします。",
        detail: "切断時と復帰時に macOS の通知を表示します。",
      },
      errors: {
        title: "エラーはわかりやすい言葉で",
        body: "パスワードや共有シークレットの誤り、ブロックされたネットワーク、オフライン。それぞれに専用のメッセージと対処法があります。",
        detail: "[[Update Password]] や [[Try Again]] などのボタンで次の操作へ進めます。",
      },
      network: {
        title: "ネットワークが戻るまで待つ",
        body: "Wi-Fi がなくても、Mac がオンラインに戻ればすぐに再接続します。",
        detail: "失敗扱いにせず [[Waiting for Network…]] と表示します。",
      },
      killSwitch: {
        title: "通信を外に漏らさない",
        body: "VPN が切れたら、再接続するまでインターネット通信を止めます。保護されない通信は出ていきません。",
        detail: "Kill switch：すべての通信を VPN 経由にする構成で使えます。",
      },
      keychain: {
        title: "パスワードは安全に保管",
        body: "パスワードと共有シークレットは macOS のキーチェーンに保存され、ファイルには残りません。",
        detail: "秘密情報ごとにキーチェーン項目を作り、アンインストール時に削除します。",
      },
    },
  },
  screens: {
    eyebrow: "セットアップ",
    title: "設定は一度だけ。IT から届いた情報で。",
    newConfigurationAlt:
      "New Configuration フォーム：表示名、サーバアドレス、アカウント名、パスワード、共有シークレット、Send all traffic over VPN。",
    newConfigurationCaption: "管理者から受け取ったサーバ、アカウント、共有シークレットを入力するだけです。",
    settingsAlt: "ダークモードの Settings ウィンドウ：接続の設定、Kill switch、Notifications。",
    settingsCaption: "VPN が切れたときにインターネットを止めるか、どの通知を受け取るかを選べます。",
  },
  install: {
    eyebrow: "インストール",
    title: "3 ステップで使えます。",
    steps: [
      { title: "ダウンロード", body: "{dmg} をダウンロードします。" },
      { title: "アプリケーションへ", body: "ファイルを開き、**TOMOSIA VPN** を **アプリケーション** へドラッグします。" },
      { title: "開く", body: "アプリを開きます。macOS にブロックされたら、アプリを右クリックして **開く** を選びます。" },
    ],
    uninstall: {
      summary: "アンインストールするには",
      body: "ターミナルで、通常のユーザーとして次のコマンドを実行します。アプリ、付属のコマンドラインツール、設定、キーチェーンに保存したパスワードを削除します。",
      copy: "コピー",
      copied: "コピーしました",
    },
  },
  cta: {
    title: "どこで働いても、つながったまま。",
    body: "ダウンロードしてスイッチを入れたら、あとは仕事に戻るだけ。",
  },
  backToTop: "ページの先頭へ",
  footer: {
    tagline: "社内 VPN を、スイッチひとつで。",
    github: "GitHub",
    releases: "すべてのリリース",
    developers: "開発者向け",
  },
};

export default ja;
