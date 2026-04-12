# Google Calendar 連携セットアップガイド (Phase 20)

> 💡 **Phase 20e 以降の推奨**: `docs/ICAL_FEED.md` を参照してください。
> iCalendar 購読 URL 方式なら **審査不要 / Workspace 不要 / 無料 / Apple・Google・Outlook 全対応 / チーム相互購読可** で、このページの手順をすべてスキップできます。
> このページは Workspace 契約 or OAuth verification を通す前提の補助ルート用に残しています。

Phox CRM の Redial (掛け直し予定) を **ユーザー個人の Google カレンダー** に自動
同期するための、GCP プロジェクトと OAuth クライアントの初期設定手順。

開発時は `GCAL_MODE=mock` で完結するので、この手順が必要なのは **実 Google
アカウントで試したい時** / **本番運用** のみ。

---

## 1. 前提

- Google アカウントを持っている
- GCP プロジェクトの作成権限がある (個人アカウントならデフォルトで OK)
- Calendar API 利用は無料枠内 (1 日 1,000,000 リクエスト) のみ使うので **課金
  アカウントは不要**
- 自分 1 人 (または数人の Test users) のアカウントで試す分には **アプリ審査は不要**
  (Testing モードのまま)

## 2. GCP プロジェクト作成

1. https://console.cloud.google.com/projectcreate を開く
2. プロジェクト名: `phox-dev` など (組織は「組織なし」で OK)
3. 「作成」ボタン

## 3. Google Calendar API を有効化

1. プロジェクトを選択した状態で
   https://console.cloud.google.com/apis/library/calendar-json.googleapis.com
2. 「有効にする」をクリック

## 4. OAuth 同意画面を設定

https://console.cloud.google.com/apis/credentials/consent

| 項目 | 値 |
|---|---|
| User Type | **External** |
| App name | `Phox CRM (dev)` |
| User support email | 自分のメールアドレス |
| App logo | (空で OK) |
| App domain | (空で OK) |
| Developer contact information | 自分のメールアドレス |

次の Scopes ページ:

- 何も追加しなくて OK (backend の oauth2.Config 側で指定する)
- `Save and continue`

次の Test users ページ:

- **自分の Google アカウントメールを追加する** ← 重要
- Testing モードでは Test users に追加されたアカウントのみ連携可能
- 最大 100 人まで登録可能

次のまとめページ → `Back to dashboard`

**Publishing status は `Testing` のままで OK**。`In production` にしないと
他の Google ユーザーはこのアプリを使えないが、自分用なら不要。

> ⚠️ **Testing モードの注意**: refresh_token が **7 日で自動失効** する Google
> の制約がある (2022 年以降)。7 日経ったら `/settings` から再連携が必要。
> 本番運用では `In production` に変更する必要があるが、その場合は審査フロー
> (簡単な動画 + domain verify) を経る必要があるので別スコープ扱い。

## 5. OAuth 2.0 クライアント ID を作成

1. https://console.cloud.google.com/apis/credentials
2. 「+ 認証情報を作成」→「OAuth クライアント ID」
3. Application type: **Web application**
4. Name: `phox-customer local`
5. **Authorized redirect URIs** に次を追加:
   ```
   http://localhost:8082/oauth/google/callback
   ```
   (本番では `https://phox.example.com/oauth/google/callback` など、
   phox-customer が listen するホスト名を指定)
6. 「作成」
7. ダイアログに表示される **Client ID** と **Client secret** をコピー
   (後で参照する場合は認証情報一覧から再取得可能)

## 6. phox-customer に credentials を埋める

本リポジトリでは `app.env.local` (gitignore 対象) に developer 別の override を
置く。`app.env` は commit 対象で dev defaults (mock + dummy) が入っている。

```bash
cd phox-customer
cp app.env.local.example app.env.local
```

`app.env.local` を編集:

```ini
GCAL_MODE=real
GOOGLE_OAUTH_CLIENT_ID=1234567890-xxxxxxx.apps.googleusercontent.com
GOOGLE_OAUTH_CLIENT_SECRET=GOCSPX-xxxxxxxxxxxxxxxx
GOOGLE_OAUTH_REDIRECT_URL=http://localhost:8082/oauth/google/callback
```

backend を再起動:

```bash
# 既存プロセスを止めて再起動
lsof -ti:8082 | xargs kill
go run main.go reindex.go
```

起動ログに次のような行が出ていれば OK:

```
{"level":"info","message":"Loaded overrides from app.env.local"}
{"level":"info","message":"GCal client: real","client_id_prefix":"1234567890...","redirect":"http://localhost:8082/oauth/google/callback"}
```

もし `GCAL_MODE=real requires a real GOOGLE_OAUTH_CLIENT_ID` というエラーで
fail fast した場合は、`app.env.local` の値がまだ `dummy-` プレフィックスに
なっているか、読み込めていない。

## 7. UI から動作確認

1. http://localhost:3000 にログイン
2. 右上の歯車アイコン → `/settings` へ
3. 「Google カレンダー連携」カードで「Google カレンダーに連携」ボタンをクリック
4. Google の認可画面に遷移 → あなたの Google アカウントを選択
5. 「Phox CRM (dev) が次のことをできるようにします:」で許可
6. phox-ui の `/settings?google=connected` に戻る
7. 「連携済み」と連携した Google アカウントのメールが表示されていれば成功
8. 顧客詳細画面 → 「掛け直し予定」 → 「新規予約」
9. 日時/メモ/電話を入れて保存
10. Google カレンダー (https://calendar.google.com/) を開いて、`[Phox] xxx へ
    掛け直し` というイベントが入っているか確認
11. phox-ui の Redial カードで「同期済」バッジが出ていることを確認

## 8. トラブルシューティング

| 症状 | 原因 | 対処 |
|---|---|---|
| 認可画面で `Error 403: access_denied` | Test users に自分のアカウントが登録されてない | OAuth 同意画面の Test users に自分のメールを追加 |
| `missing refresh token` redirect に戻る | Google が refresh_token を返さなかった (同意解除せずに再認可した) | Google アカウント設定の `Third-party apps with account access` から Phox を削除 → 再度連携 |
| `/settings?google=error&reason=exchange_failed` | Client ID / Secret が間違っている | `app.env.local` の値と GCP Console の値を見比べる |
| 7 日後に再同期が必要 | Testing モードの refresh_token 失効 | `/settings` から再連携 / または Publishing status を production に |
| `GCAL_MODE=real requires a real GOOGLE_OAUTH_CLIENT_ID` | config validation に引っかかった | `app.env.local` が読み込まれてない or `dummy` プレフィックスが残ってる |

## 9. integration test (実 Google API 動作確認)

build tag `gcal_integration` 付きのテストが `internal/gcal/` にある。
実 Google API に対して CRUD を走らせるので、一度 `/settings` から連携を済ませた
後のテスト refresh_token が必要。

```bash
# 1. 自分のアカウントで連携を済ませる (UI から)
# 2. DB から refresh_token を取り出し (復号は crypto.Cipher.DecryptString 経由)
# 3. 環境変数に入れて build tag 付きで実行:

GCAL_INTEGRATION_REFRESH_TOKEN="1//0g..." \
GCAL_INTEGRATION_CLIENT_ID="..." \
GCAL_INTEGRATION_CLIENT_SECRET="..." \
go test -tags gcal_integration ./internal/gcal/...
```

詳細は `internal/gcal/integration_test.go` 冒頭のコメント参照。
CI では走らせない (実 Google API は flaky で課金/quota 的にも不適切)。

## 10. 本番運用に移行する時の追加作業 (参考)

- OAuth 同意画面の Publishing status を `In production` に変更
- その際 Google の審査 (動画 + 公開ドメイン verify + プライバシーポリシー URL) を通す必要あり
- 本番 redirect URL (https://phox.example.com/oauth/google/callback) を Authorized redirect URIs に追加
- `GCAL_TOKEN_KEY` は本番では `openssl rand -base64 32` で生成した値に変更
- `app.env.local` は本番サーバーでは secret manager (GCP Secret Manager / AWS
  Secrets Manager / env var) から注入
