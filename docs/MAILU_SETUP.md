# mailu 連携セットアップ (Phase 14b)

phox-customer の dev 既定は **MailHog** (fast, IMAP 非対応) ですが、
`app.env.local` の override で **実 mailu インスタンス** に接続できます。
これにより以下が可能:

- **SMTP 送信の本番疎通テスト** (SendEmailDialog → 実 mailu → 本物の受信先)
- **IMAP 取込みのテスト** (Sent フォルダ polling で外部クライアント送信を Activity に / INBOX polling で顧客からの返信を email_received に)
- `/activity.v1.ActivityService/CreateActivityEmailSent` の real-world validation

本文書はその切替え手順を説明します。

## 前提

- mailu インスタンスが立ち上がっていて、dev 開発機 (localhost) から
  **TCP 到達可能** であること
  - 公開ホスト名 (例: `mail.example.com`) なら TLS 証明書は Let's Encrypt 等で
    signed、そのまま使える
  - LAN 内 IP (例: `192.168.1.10`) で自己署名証明書ならテスト時に
    `IMAP_TLS_INSECURE_SKIP_VERIFY=true` を使う必要あり
- mailu の admin 画面にアクセスできること

## mailu 側の設定 (1 回だけ)

### 1. phox 用サービスアカウントを作成

mailu admin UI → `Users` → `Add user`

| 項目 | 値 |
|---|---|
| Email | `phox@<your-domain>` (例: `phox@example.com`) |
| Password | 強力なランダム (推奨 32 char 超) |
| Sender Restrictions | **解除** (= arbitrary From アドレスで送信可能に) |

**なぜ restriction 解除が必要か**: phox は `CreateActivityEmailSent` で
**ユーザー本人の email claim を From ヘッダに焼き付ける** (Phase 11 の設計)。
mailu 既定の sender restriction はこれを弾くので、service account にだけ
除外を入れる必要がある。

mailu の restriction 設定:
- admin UI の `User` → `phox@example.com` → `Global sender` or
  `Allowed senders` に `*@example.com` (domain 内 arbitrary) を追加
- 上記 GUI に該当項目が無い場合、`mailu/mailu.env` で
  `MESSAGE_SIZE_LIMIT` 付近の設定、または `postfix/main.cf.override` に
  `smtpd_sender_restrictions = permit_sasl_authenticated, ...` を直書き

### 2. 取込み専用 Mailbox を (任意で) 分離

phox の IMAP worker は `Sent` + `INBOX` を polling しますが、混線を避けたい場合:

- `phox@example.com` 用の別 Sub-mailbox (例: `INBOX.phox-ingest`) を作成し、
  mail forwarding rule で「phox 管理顧客からの受信だけ」そこに振り分け
- `IMAP_INBOX_MAILBOX=INBOX.phox-ingest` で polling 対象を絞る

本番では **分けることを強く推奨**。dev 疎通テストだけなら `INBOX` + `Sent` で OK。

## phox-customer 側の設定 (`app.env.local`)

既存の `app.env.local` (gitignored) に次を追加:

```ini
# --- Phase 14b: real mailu 接続 ---

# SMTP (送信): Phase 11 で既に `implicit` + 465 想定の実装あり
SMTP_HOST=mail.example.com
SMTP_PORT=465
SMTP_TLS_MODE=implicit
SMTP_USERNAME=phox@example.com
SMTP_PASSWORD=<strong-random-from-step-1>
SMTP_DEFAULT_FROM=phox@example.com

# IMAP (受信): Phase 14b の本実装で Sent + INBOX を polling
IMAP_HOST=mail.example.com
IMAP_PORT=993
IMAP_TLS_MODE=implicit
# 自己署名証明書 (LAN 内) の場合のみ true
# IMAP_TLS_INSECURE_SKIP_VERIFY=true
IMAP_USERNAME=phox@example.com
IMAP_PASSWORD=<strong-random-from-step-1>
IMAP_SENT_MAILBOX=Sent
IMAP_INBOX_MAILBOX=INBOX
# 30s / 1m / 5m — dev では短く、prod では 1〜5 分推奨
IMAP_POLL_INTERVAL=30s
# デフォ "system" (000003_create_activity mig で seed 済の行) のまま
# IMAP_INGEST_USER_ID=system
```

`app.env.local` は `.gitignore` されているので commit されません。
Phase 20 の Google OAuth credentials と同じファイルに書けば OK。

## phox-customer 再起動

```bash
cd phox-customer
lsof -ti:8082 | xargs kill 2>/dev/null
go run main.go reindex.go
```

起動ログに次が出れば接続試行成功:

```
{"level":"info","message":"SMTP client initialized","host":"mail.example.com","port":465,"tls_mode":"implicit"}
{"level":"info","message":"IMAP worker: starting polling loop","host":"mail.example.com","port":993,"sent":"Sent","inbox":"INBOX","interval":"30s"}
```

その後 polling サイクルで:

- `IMAP worker: ingested activity` → 取込み成功ログ
- `IMAP worker: no matching customer — skipping` → Customer.mail / Contact.mail
  にヒットせず skip (正常)
- `IMAP worker: tick failed` → 接続 or fetch 失敗 (要調査)

## 動作確認 (手動 smoke)

### 送信パスの確認

1. phox-ui (`http://localhost:3000`) にログイン
2. 顧客詳細 (適当な顧客) → 代表者情報のメールアイコン → SendEmailDialog
3. To に **自分が実際に受信できる gmail** などを入れて送信
4. 受信箱にメールが届くことを確認 (From が自分の Keycloak email の想定)
5. mailu admin UI の logs でメール送信履歴を確認

### 受信取込みパスの確認 (INBOX)

1. phox-customer DB の `Customer` テーブルに `mail='test-customer@gmail.com'`
   の行があることを確認 (なければ UI で Customer を作って mail を設定)
2. その gmail から phox service account (`phox@example.com`) 宛に
   適当なメールを送信
3. 30 秒待つ (polling interval)
4. phox-customer ログに `IMAP worker: ingested activity type=email_received` が出る
5. phox-ui の顧客詳細 → 活動履歴 → 「メール」フィルタ → 受信行が追加されている

### 受信取込みパスの確認 (Sent)

外部クライアント (Gmail 等) から phox service account として適当な顧客宛に送信
→ その送信が `Sent` フォルダに入る → phox の polling が拾って `email_sent`
として取込まれる。

## MailHog モードに戻す

`app.env.local` の IMAP_HOST / SMTP_HOST を元に戻すか、IMAP_HOST 行を削除:

```ini
# IMAP_HOST をコメントアウトするだけで worker は起動しなくなる
# IMAP_HOST=mail.example.com
```

backend 再起動で `IMAP worker: ...` ログが出なくなれば OK (Enabled=false で no-op)。

E2E テスト (`phox-e2e`) は常に MailHog 前提なので、E2E を回す時は必ず mock
モード + MailHog 接続に戻すこと。

## トラブルシューティング

| 症状 | 対処 |
|---|---|
| `imap: login: Bad username or password` | サービスアカウントのパスワードを mailu admin で確認。管理画面で手動ログインして動くことを確認 |
| `x509: certificate signed by unknown authority` | LAN 内自己署名証明書の場合。`IMAP_TLS_INSECURE_SKIP_VERIFY=true` を付ける (dev/LAN のみ許容) |
| `imap: select "Sent": NO` | mailbox 名がローカライズされている可能性 (例: mailu の日本語 UI では `送信済み`)。mailu admin の IMAP 画面で実際の folder 名を確認して `IMAP_SENT_MAILBOX` で指定 |
| polling しても `ingested activity` ログが出ない | (1) Customer.mail / Contact.mail にマッチしない → 顧客の mail アドレスと送信先/送信元が一致しているか確認 (2) 24 時間以上前のメールは拾わない (SEARCH SINCE の範囲外) |
| SMTP `sender restriction` で reject される | mailu の service account に arbitrary sender permission を付与 (本ドキュメント § "mailu 側の設定 § 1") |
| phox-customer が起動後即 crash | 必須 config (GCAL_TOKEN_KEY 等) が欠けている。既存 Phase 20 の要件を確認 |

## 実装参照

- `phox-customer/internal/mail/smtp.go` — SMTP client (Phase 11)
- `phox-customer/internal/mail/imap.go` — IMAP client wrapper (Phase 14b)
- `phox-customer/internal/mail/imap_worker.go` — polling loop + ingest (Phase 14b)
- `phox-customer/internal/mail/imap_test.go` — `imapmemserver` を使った unit test
- `phox-customer/internal/util/config.go` — env var マッピング (`IMAP_*`)
- `phox-customer/main.go` — worker の起動配線 (`mail.NewIMAPWorker(...)` + errgroup)
