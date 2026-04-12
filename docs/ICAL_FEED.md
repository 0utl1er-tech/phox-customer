# iCalendar 購読 URL (Phase 20e)

Phox CRM の掛け直し予定 (Redial) を、ユーザーのカレンダーアプリに自動同期する
仕組みのガイド。

## なぜこの方式か

Phase 20 で構築した Google Calendar OAuth 連携 (`docs/GOOGLE_OAUTH_SETUP.md` 参照)
は `calendar.events` がセンシティブ scope のため、production 公開に Google の
OAuth verification (2〜4 週間) が必須で、運用負担が重い。

一方 **iCalendar (.ics) 購読 URL** は RFC 5545 で標準化された方式で:

- Apple Calendar / Google Calendar / Outlook / Thunderbird / Fastmail / 主要
  カレンダーアプリが全て対応
- 審査も OAuth も不要
- Workspace 契約不要 (= 無料)
- URL シェアだけでチーム相互購読 (カレンダーを重ねて見れる)
- 読み取り専用のため、漏洩時の被害は限定的 (= 自分の掛け直し予定の閲覧のみ)

Phase 20e 以降はこちらを **メイン導線** として扱い、Google OAuth 連携コードは
Workspace 契約 or verification 取得時のための補助として残している。

## 仕組み

```
phox-customer が GET /ical/{token}.ics を公開 (http/https)
  ↓
CRM ユーザーが /settings で「購読URLを生成」
  → base64 (32byte) トークンが発行される
  → UI には `webcal://localhost:8082/ical/{token}.ics` が表示される
  ↓
ユーザーがこの URL をカレンダーアプリに「照会/購読」登録
 (または URL を直接クリックすると OS が Calendar.app 等を自動起動)
  ↓
カレンダーアプリが `webcal://` を内部で `http(s)://` に置換して
  〜15 分間隔で URL を GET
  ↓
phox-customer が user_id を解決 → 過去 90 日 + 全未来の Redial を
RFC 5545 準拠の iCalendar で返す
  ↓
カレンダーアプリが解釈 → 端末のカレンダービューに反映
```

### `webcal://` スキームについて

Phase 20e ではトークン URL を `webcal://` で返します。これは 1999 年から Apple
が採用している calendar feed 用の慣例スキームで、全主要カレンダーアプリが対応:

- **ブラウザでクリック** → OS が「Apple Calendar で開きますか?」とプロンプト
  → 1 クリックで購読完了
- **カレンダーアプリに貼り付け** → アプリ側が `webcal://` を `http(s)://`
  に置換して GET する
- **`http://` と違って** クリックしても .ics の生テキストがブラウザに
  dump されず、意図通りカレンダーアプリが起動する

backend 自体は HTTP で listen しているので、`webcal://` と `http://` の両方で
feed は取得できる (E2E テストでは webcal:// を http:// に変換して Playwright
で直接 fetch している)。

**token 自体が認証** なので、URL を共有した相手は全員あなたの掛け直し予定を
見られる。漏れた or チームを抜けた時は UI で「URL を再生成」すれば古い URL は
即座に 404 になる。

## 購読手順

### macOS Calendar.app

1. メニューバー → **ファイル** → **新規照会カレンダー...**
2. `/settings` でコピーした URL を貼付 → **照会**
3. ダイアログで:
   - アカウント: このコンピュータ (または iCloud)
   - 自動更新: **5 分 / 15 分 / 1 時間 / 1 日 / 1 週間** から選択 (15 分推奨)
   - 通知: 任意
4. **OK**

以降、サイドバーに「Phox — {あなたの名前} の掛け直し予定」カレンダーとして表示される。
iCloud アカウント配下に入れれば iPhone / iPad / Apple Watch にも自動伝播する。

### iPhone / iPad

1. 設定アプリ → **カレンダー** → **アカウント** → **アカウントを追加**
2. **その他** → **照会するカレンダーを追加**
3. サーバー欄に URL を貼付 → **次へ** → **保存**

更新間隔は iOS が自動で決める (〜15 分)。

### Google Calendar

1. 左サイドバーの「**他のカレンダー**」の右 **+** → **URL で追加**
2. URL を貼付 → **カレンダーを追加**

⚠️ **Google Calendar の購読 feed は更新が最大 24 時間遅延する** ことに注意。
即時反映したい場合は Apple Calendar か Thunderbird を使うこと。

### Microsoft Outlook

1. Outlook → 「カレンダー」ビュー
2. ホーム → **カレンダーを開く** → **インターネットから**
3. URL を貼付 → **OK**

### Thunderbird

1. カレンダービュー → 左サイドバーで右クリック → **新しいカレンダー...**
2. **ネットワーク上** → **次へ**
3. 形式 **iCalendar (ICS)** → 場所に URL を貼付 → **次へ** → **完了**

## セキュリティ

- URL はシークレット。チャット / Git コミット / スクリーンショット等で漏らさない
- 漏れたと判明したら即 `/settings` → 「URL を再生成」(古い URL は即 404)
- URL = read-only credential なので、漏洩しても「その人が redial を閲覧できる」
  だけ。redial の作成・変更はできない
- backend は token のログを一切残さない (access log に user_id のみ記録)

## チーム利用の tips

1. チームメンバー A が `/settings` で URL を生成 → ex: `https://.../ical/abc.ics`
2. A が B に URL を slack 等で共有 (セキュアなチャネル推奨)
3. B が自分の Apple Calendar にそれを購読 → B のカレンダーに A の掛け直し予定が
   別カラーで表示される
4. B が自分の feed URL も A にシェアし返せば相互可視
5. 全員のを重ねることで「今週 チーム全体でどれぐらい redial があるか」が一目瞭然

Phox 側で authz や別モードは不要。URL シェアだけで完結する。

## 更新間隔とレスポンス

- **Apple Calendar**: デフォ 15 分、手動変更可 (5 分 〜 1 週間)
- **iPhone**: 自動、おおよそ 15 分
- **Google Calendar**: 最大 24 時間 (制御不可)
- **Outlook**: 通常 1 時間
- **Thunderbird**: デフォ 30 分、手動変更可

サーバー側は `Cache-Control: private, max-age=900` + `ETag` を返すので、
変更がなければ 304 Not Modified でバンド幅節約される (クライアントが対応していれば)。

## トラブルシューティング

| 症状 | 原因 / 対処 |
|---|---|
| URL を貼っても購読されない | URL 末尾の `.ics` 拡張子が有ると無いで差がある場合がある。backend はどちらでも受け付けるが、アプリによっては拡張子必須 |
| Apple Calendar で「認証エラー」 | token が revoke 済み / 再生成された / 無効化された。Phox の /settings で最新 URL を確認して再登録 |
| Google Calendar に反映されない | 更新は最大 24 時間遅延。気長に待つか Apple Calendar を使う |
| 予定が消えた | `/settings` → 「無効化」が押された可能性。再生成する |
| リモートでもイベントが古いまま | カレンダーアプリ側のキャッシュ。Apple Calendar は Calendar.app で右クリック → 「更新」で強制更新可 |

## 内部仕様 (参考)

- 実装: `phox-customer/internal/ical/feed.go`, `handler.go`
- Connect RPC (CRUD): `phox-customer/internal/service/icalfeed/`
- proto: `phox-customer/proto/icalfeed/v1/icalfeed.proto`
- iCalendar library: `github.com/arran4/golang-ical`
- Token: crypto/rand 32 byte → base64 URL-safe (43 char)
- RFC 5545 準拠:
  - `PRODID: -//Phox//Phox CRM iCal Feed 1.0//EN`
  - `DTSTART`/`DTEND` は UTC Z 形式 (TZID と混在させない)
  - `UID: phox-redial-{uuid}@phox.local` で安定
  - `METHOD:PUBLISH` (サーバー push 型)
- 含まれる Redial: 過去 90 日 + 全未来 (最大 1000 件)
- Frontend カード: `phox-ui/src/components/crm/ical-feed-card.tsx`
