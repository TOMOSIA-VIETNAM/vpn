<p align="center">
  <img src="assets/logo.png" width="96" height="96" alt="TOMOSIA VPN logo" />
</p>

<h1 align="center">TOMOSIA VPN</h1>

<p align="center">
  <a href="README.md">Tiếng Việt</a> · <a href="README.en.md">English</a> · <b>日本語</b>
</p>

<p align="center">
  メニューバーで使う macOS 向け L2TP/IPsec VPN — 安定した接続と分かりやすいエラー表示。
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-v0.6.1-blue.svg" alt="Version 0.6.1" />
  <img src="https://img.shields.io/badge/macOS-12.0+-black.svg" alt="macOS 12+" />
  <img src="https://img.shields.io/badge/arch-Apple%20Silicon%20%7C%20Intel-success.svg" alt="Architecture" />
</p>

---

## 概要

macOS 標準の L2TP VPN（*システム設定 → VPN*）は、ファイアウォールのあるオフィスや公衆 Wi-Fi で切れやすく、失敗しても原因がほとんど分かりません。

TOMOSIA VPN はその代わりになります。

- メニューバーのスイッチ 1 つで VPN をオン/オフ。複数の構成を管理できます。
- 接続が切れると自動で再接続し、切れたときと戻ったときに通知します。
- パスワード違い、共有シークレット違い、VPN を遮断するネットワークなど、原因を具体的に表示します。
- パスワードと共有シークレットは macOS のキーチェーンに保存し、設定ファイルには書きません。

動作環境：macOS 12 以降（Apple Silicon / Intel）。

---

## インストール

### 方法 1 — .dmg ファイル（推奨）

1. [`TOMOSIA-VPN.dmg`](https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download/TOMOSIA-VPN.dmg) をダウンロードします（常に最新版）。
2. 開いて **TOMOSIA VPN** を **アプリケーション** フォルダへドラッグします。
3. アプリを開きます。初回のみ：
   - 「開発元を検証できません」と表示された場合は、アプリを右クリック → **開く**。
   - 接続用ヘルパー（`/usr/local/bin/vpn`）のインストールのため、管理者パスワードを 1 回だけ求められます。
   - 通知の許可を求められたら **許可** を選びます（VPN が切れたときに通知されます）。

### 方法 2 — ターミナルで 1 行

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install.sh | bash
```

Apple Silicon / Intel を自動判別し、アプリとヘルパーをインストールしてアプリを開きます。

---

## 使い方

ネットワーク管理者から次の 4 つを受け取ってください：**サーバーアドレス**、**共有シークレット（PSK）**、**アカウント名**、**パスワード**。

1. メニューバーの TOMOSIA VPN アイコンをクリックします。
2. **Configurations** の横の **+** をクリックします。
3. 入力します：
   - **Display name**：任意の名前（例：`オフィス`）。
   - **Server address**：VPN の IP アドレスまたはホスト名。
   - **Account name** と **Password**：VPN のログイン情報。
   - **Shared secret**：IPsec の事前共有鍵。
   - **Send all traffic over VPN**：オンですべての通信を VPN 経由に、オフで社内ネットワーク宛ての通信だけを VPN 経由にします。
4. **Create** をクリックし、構成のスイッチをオンにして接続します。

構成の編集・削除は、行の **…** をクリックするか右クリックします。先に VPN をオフにしてください。

### メニューバーのアイコン

| アイコン | 意味 |
|---|---|
| 塗りつぶしの盾 | 接続中 |
| 薄い輪郭の盾 | 接続処理中 |
| 輪郭の盾 | 未接続 |
| `!` 付きの輪郭の盾 | エラー：接続に失敗した、または VPN が切れて再接続を待っている |

VPN が切れたとき・戻ったときに macOS の通知が届きます。届かない場合は、アプリの歯車アイコン（**Settings**）→ **Notifications** → **Open Settings…** から TOMOSIA VPN の通知を許可してください。

### 設定（Settings）

ポップオーバー下部の歯車アイコンから開きます。変更は次回の接続から反映されます。

- **MTU**：通常は `1280`（既定値）のまま。管理者の指示がある場合のみ `1400` にします。
- **Kill switch**：「Send all traffic」の VPN が切れたとき、再接続までインターネットを遮断し、VPN 外への通信を防ぎます。
- **Verbose log**：問題調査のために管理者へ渡す詳細ログを記録します。
- **Notifications**：通知が許可されているかを表示します。

---

## トラブルシューティング

ポップオーバーのエラーカードに原因が表示され、**Try Again** / **Update Password** ボタンが使えます。

| メッセージ | 原因 | 対処 |
|---|---|---|
| Authentication Failed | アカウント名またはパスワードの誤り | **Update Password** から入力し直す |
| IKE Handshake Failed | 共有シークレットの誤り | **…** → **Edit…** から共有シークレットを入力し直す |
| Server Not Responding | このネットワークが VPN（UDP 500/4500）を遮断している、またはサーバー停止 | 別のネットワーク（テザリングなど）を試す、管理者に確認する |
| Waiting for Network | Mac がオフライン | ネットワークが戻ると自動で再接続します |
| VPN Stopped Unexpectedly | VPN プロセスが停止した | スイッチをもう一度オンにする |

それでも接続できない場合は、ターミナルで実行してください：

```bash
vpn diagnose    # ネットワーク経路、DNS、UDP 500/4500 を確認
vpn repair      # 切断後にネットワークがおかしい場合、ルート/DNS を元に戻す
```

`vpn diagnose` と `vpn logs` の出力を管理者に送ってください。

---

## アップデートとアンインストール

**アップデート：** 最新の `.dmg` をダウンロードして **アプリケーション** へ上書きするか、上の 1 行インストールコマンドを再実行します。現在のバージョンはポップオーバー下部に表示されます（例：`v0.6.1`）。

**アンインストール**（アプリ、ヘルパー、構成、キーチェーンに保存したパスワードを削除）：

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/uninstall.sh | bash
```

---

## コマンドライン（上級者向け）

アプリでできる操作はすべて `vpn` コマンドでも行えます：

```bash
vpn profile add work --server vpn.example.com   # 共有シークレットを入力
vpn account add work tanaka --default           # パスワードを入力
vpn connect
vpn status
vpn disconnect
```

| コマンド | 内容 |
|---|---|
| `vpn connect [--profile <名前>]` / `vpn disconnect` | 接続 / 切断 |
| `vpn status` | 状態と IP アドレス |
| `vpn profile list` | 構成の一覧（`*` = 選択中） |
| `vpn profile add <名前> --server <host>` | 構成を追加 |
| `vpn profile edit <名前> [--server host] [--user 名前] [--full-tunnel=bool] [--set-psk]` | 構成を編集（保存済みのシークレットは維持） |
| `vpn profile rename <名前> [表示名]` | 表示名を変更 |
| `vpn profile remove <名前>` | 構成とキーチェーンのシークレットを削除 |
| `vpn account add <構成> <user>` | アカウントを追加 |
| `vpn mtu [1280\|1400]`、`vpn killswitch [on\|off]`、`vpn verbose [on\|off]` | 全体設定 |
| `vpn diagnose`、`vpn logs -f`、`vpn repair` | 診断とネットワーク復旧 |
| `vpn update` | ヘルパーを更新（署名を検証） |
| `vpn version` | バージョン |

---

## コントリビュート

ソースからのビルド、テスト、リリース手順は [CONTRIBUTING.md](CONTRIBUTING.md) を参照してください。
