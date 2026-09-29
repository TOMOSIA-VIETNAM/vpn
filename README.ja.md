<p align="center">
  <img src="assets/logo.png" width="96" height="96" alt="TOMOSIA VPN logo" />
</p>

<h1 align="center">TOMOSIA VPN</h1>

<p align="center">
  <a href="README.md">Tiếng Việt</a> · <a href="README.en.md">English</a> · <b>日本語</b>
</p>

<p align="center">
  <b>macOS ネイティブの L2TP/IPsec VPN クライアント — 安定・安全で、外部ソフトウェアに依存しません。</b>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-v0.6.1-blue.svg" alt="Version 0.6.1" />
  <img src="https://img.shields.io/badge/macOS-12.0+-black.svg" alt="macOS 12+" />
  <img src="https://img.shields.io/badge/arch-Apple%20Silicon%20%7C%20Intel-success.svg" alt="Architecture" />
  <img src="https://img.shields.io/badge/protocol-L2TP%20%2F%20IPsec-orange.svg" alt="L2TP/IPsec" />
</p>

---

## 💡 概要

macOS 標準の VPN（*システム設定 → VPN → L2TP over IPsec*）は接続が不安定になりがちで、公共 Wi-Fi やファイアウォールのあるオフィスネットワークでは繋がりにくく、エラーも原因の分からない汎用メッセージしか表示されません。

**TOMOSIA VPN** はプロトコルスタック全体（IKEv1、ESP、L2TP、PPP）を自前で実装し、安定して途切れにくく、原因を診断しやすい接続を実現します。
- 🖥️ **メニューバーアプリ（SwiftUI）**：ワンクリックで接続/切断、複数のプロファイルとアカウントの管理、状態の即時表示。
- ⚡ **CLI エンジン（`vpn`）**：Go 製の高性能な接続マネージャー。Docker・WireGuard・strongSwan・xl2tpd・pppd は一切不要で、単体で動作します。

---

## 🚀 インストール

**方法 1 — `.dmg`（一般的な Mac アプリと同じ）：**

1. [`TMS-VPN.dmg`](https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download/TMS-VPN.dmg) をダウンロードします（常に最新版）。
2. 開いて **TOMOSIA VPN** を **アプリケーション** フォルダへドラッグします。
3. アプリを起動します。初回起動時に、`vpn` コアを導入するため管理者パスワードが一度だけ求められます。開発元を確認できないという警告が出た場合は、アプリを右クリック → **開く** を選んでください。

`.dmg` を自分でビルドする場合：`VERSION=1.2.3 ./make-dmg.sh`（出力：`build/TMS-VPN.dmg`）

**方法 2 — スクリプト：**

**自動インストール（Apple Silicon M1/M2/M3… または 2017 年以降の Intel Mac を自動判別）：**

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install.sh | bash
```

**アーキテクチャを指定する場合：**

```bash
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install-arm64.sh | bash   # Apple Silicon (M1/M2/M3...)
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install-intel.sh | bash   # Intel Mac
```

インストールの確認：
```bash
vpn version    # 出力: vpn v0.6.1
```
メニューバーアプリのバージョンは、ポップオーバー左下に表示されます（例：`v0.6.1`。著作権表示の横）。

---

## 🎯 使い始める

ネットワーク管理者から次の 4 つの情報を受け取ってください：**サーバーアドレス**、**IPsec 事前共有鍵（PSK）**、**ユーザー名**、**パスワード**。

### 方法 1：メニューバーアプリ（推奨）

1. `アプリケーション` または Spotlight から **TOMOSIA VPN** を開きます。
2. メニューバーの TOMOSIA VPN アイコンをクリック → **Add**。
3. 次の項目を入力します：
   - **Display name**：分かりやすい名前（例：`会社`）。
   - **Server address**：VPN サーバーの IP アドレスまたはドメイン。
   - **Account name & Password**：ログイン用アカウント。
   - **Shared secret (PSK)**：IPsec の共有鍵。
4. **Create** を押し、**スイッチをオン**にして接続します！

> 🔒 *パスワードと PSK は macOS の Keychain に安全に保存され、設定ファイルに平文で残ることはありません。*

---

### 方法 2：コマンドライン（CLI）

開発者や、スクリプトによる自動化に向いています：

```bash
# 1. サーバープロファイルを追加（PSK は CLI が安全に入力を求めます）
vpn profile add work --server vpn.example.com

# 2. ユーザーアカウントを追加（パスワードは CLI が入力を求めます）
vpn account add work alice --default

# 3. 接続（バックグラウンドで動作し、すぐに結果を返します）
vpn connect

# 4. 状態と IP を確認
vpn status
curl -4 https://ifconfig.co
```

---

## ✨ 主な特長

- 🛡️ **厳しいネットワークも突破（NAT-T / ポート切替）**：オフィスのルーターが UDP/500 を遮断する場合（IPsec パススルーの問題）は IKE ポートを自動で切り替え、UDP/4500 での NAT-T に対応し、MTU も自動調整します。
- 🔄 **常時接続と自動再接続**：IKE DPD、L2TP Hello、PPP LCP Echo に自動応答し、セッションを切らずにバックグラウンドで定期的に鍵を更新（Rekeying）します。
- 🌐 **スマートな DNS とルーティング**：VPN ごとに DNS を自動設定し、Private DNS の優先に対応。接続/切断時に macOS の DNS キャッシュも消去します。
- 🛑 **キルスイッチ**：フルトンネル VPN が切れて再接続している間、外向きの通信をすべて遮断するオプション。
- 🔐 **システムレベルのセキュリティ**：
  - IKE/ESP/DH の鍵やパスワードなど機密メモリは使用後にゼロ埋めします。
  - 最小権限：ネットワーク/DNS/utun の操作に必要な瞬間だけ権限を昇格し、すぐに降格します。
  - DoS / パーサーフラッディングを防ぐ負荷制限。
- 🔒 **プロファイルのロック**：接続中のプロファイルは、アプリでも CLI でも編集・削除できません。

---

## 📋 CLI コマンド一覧

| 操作 | コマンド | 備考 |
|---|---|---|
| **接続 / 切断** | `vpn connect` | アクティブなプロファイルに接続 |
| | `vpn connect --profile <名前> --force` | 指定プロファイルを強制的に再接続 |
| | `vpn disconnect` | 切断 |
| | `vpn status` | 状態、トンネル IP、接続時間 |
| **プロファイル管理** | `vpn profile list` | プロファイル一覧（* = アクティブ） |
| | `vpn profile add <名前> --server <host>` | サーバーを追加 |
| | `vpn profile rename <名前> [新しい名前]` | アプリに表示される名前を変更 |
| | `vpn profile edit <名前> [--server host] [--user 名] [--full-tunnel=bool] [--set-psk]` | ホスト・ユーザー名・トンネルモードを変更（保存済みのパスワードとシークレットは維持） |
| | `vpn profile remove <名前>` | プロファイルと Keychain の秘密情報を削除（接続中は不可） |
| **アカウント管理** | `vpn account add <profile> <user>` | プロファイルにアカウントを追加 |
| **全体設定** | `vpn mtu [1280\|1400]` | MTU を設定（混雑しやすい 4G/PPPoE では 1280） |
| | `vpn killswitch [on\|off]` | 切断時の通信遮断を切り替え |
| | `vpn verbose [on\|off]` | プロトコルの詳細ログを切り替え |
| **診断と復旧** | `vpn diagnose` | 接続・DNS・ポート 500/4500 を総合チェック |
| | `vpn logs -f` | ログをリアルタイムで表示 |
| | `vpn repair` | ネットワークエラー後に残ったルート/DNS を掃除 |
| **更新 / 削除** | `vpn update` | 最新リリースをダウンロードして更新 |
| | `vpn uninstall` | CLI、設定、Keychain を削除 |

---

## 🛠️ トラブルシューティング

問題が起きたら、次を実行して確認します：
```bash
vpn diagnose      # 経路・DNS・UDP ポート 500 / 4500 を確認
vpn logs -f       # プロトコルの詳細を確認
```

### よくあるエラー

| エラーメッセージ | 原因 | 対処 |
|---|---|---|
| `IKE_AUTH_FAILED` / `HASH_R mismatch` | 事前共有鍵（PSK）が違う | `vpn profile add <名前> --server <host>` で PSK を入力し直す |
| `PPP_AUTH_FAILURE` / `CHAP rejected` | ユーザー名またはパスワードが違う | `vpn account add <profile> <user>` でパスワードを入力し直す |
| `IKE_TIMEOUT` / `no response` | ネットワークが UDP 500/4500 を遮断、またはサーバー IP が違う | `vpn diagnose` で確認。別のネットワークや 4G を試す |
| `already logged in` | サーバー側で前のセッションが解放されていない | 10〜15 秒待ってから再接続 |
| `DNS_FAILURE` / `ROUTE_FAILURE` | ネットワークの競合、または古い DNS が残っている | `vpn repair` でネットワーク設定を復元 |

---

## 📦 アップデートとアンインストール

- **アップデート**：
  ```bash
  vpn update          # ed25519 署名を検証して CLI を更新
  ```
  *（メニューバーアプリも更新するには、上記のワンライナーを再実行してください。アプリのフッターのバージョンと `vpn version` を見比べて、両方が同じ版か確認できます。）*

- **完全にアンインストール**：
  ```bash
  curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/uninstall.sh | bash
  ```

---

## 💻 開発者向け

### 必要な環境
- **Go**：1.22 以上（`brew install go`）
- **Xcode / Swift**：`main.swift` のビルドに Swift 6（Xcode 16 以降）

### ビルドとテスト
```bash
# ソースコードを取得
git clone https://github.com/TOMOSIA-VIETNAM/vpn.git && cd vpn

# CLI をビルドし、setuid-root でインストール
go build -o vpn ./cmd/vpn
sudo install -o root -g wheel -m 4755 vpn /usr/local/bin/vpn
id -u | sudo tee /etc/vpn-owner-uid >/dev/null && sudo chmod 600 /etc/vpn-owner-uid

# メニューバーアプリをビルド（Universal arm64 + Intel）
# アプリに表示されるバージョンは VERSION 変数から。未指定なら最新の git タグ
# （有効なタグがなければ 0.0.0）
bash build.sh                   # または: VERSION=0.6.1 bash build.sh
ditto "build/TMS VPN.app" "/Applications/TMS VPN.app"

# ユニットテストを実行
go test ./...
```

### リリース手順
新しいバージョンのタグを作成して GitHub に push すると、CI/CD が自動でビルドと署名を行います。タグは CLI（`vpn version`）とアプリ（ポップオーバーのフッター、`CFBundleShortVersionString`）の両方に反映されます：
```bash
git tag v0.6.1
git push origin v0.6.1
```

---

## 📄 ライセンスとコントリビュート
社内および macOS ユーザーのコミュニティ向けに開発されています。Pull Request / Issue を歓迎します！
