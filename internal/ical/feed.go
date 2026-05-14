// Package ical は RFC 5545 iCalendar 形式のフィード生成 + HTTP 配信を提供する。
//
// Phase 20e の目標: 各 CRM ユーザーが固有の購読 URL を持ち、その URL を Apple
// Calendar / Google Calendar / Outlook などに登録すると、そのユーザーの Redial
// (掛け直し予定) がカレンダーに自動反映される。
package ical

import (
	"bytes"
	"fmt"
	"time"

	ics "github.com/arran4/golang-ical"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

// FeedInput は 1 ユーザー分の feed を組み立てるのに必要な値。
type FeedInput struct {
	UserID       string
	UserName     string
	PhoxBaseURL  string // 例: http://localhost:3000 — deep link の構築に使う
	Redials      []db.ListRedialsByUserWithCustomerRow
	GeneratedAt  time.Time // DTSTAMP に使う (テストで固定するため param 化)
}

// BuildFeed は iCalendar (text/calendar) を byte slice として返す。
// 標準準拠:
//   - PRODID は FPI 形式 `-//Owner//Product Version//EN`
//   - DTSTART / DTEND は UTC Z 形式 (TZID と混在させない)
//   - UID は `phox-redial-{uuid}@phox.local` で安定
func BuildFeed(in FeedInput) []byte {
	cal := ics.NewCalendar()
	cal.SetProductId("-//Phox//Phox CRM iCal Feed 1.0//EN")
	cal.SetVersion("2.0")
	cal.SetMethod(ics.MethodPublish)
	cal.SetXWRCalName(fmt.Sprintf("Phox — %s の掛け直し予定", in.UserName))
	cal.SetXWRTimezone("Asia/Tokyo")
	cal.SetXWRCalDesc("Phox CRM が管理する掛け直し予定 (過去 90 日 + 今後すべて)")

	for _, r := range in.Redials {
		ev := cal.AddEvent(fmt.Sprintf("phox-redial-%s@phox.local", r.ID.String()))
		// DTSTAMP は「このイベントが最後に変更された時刻」(RFC 5545 §3.8.7.2)。
		// 旧版は time.Now() を入れていたが、これだと毎フェッチごとに全イベントが
		// 「更新された」ように見えてしまい、Google Calendar の subscription
		// sync が次第に止まる現象を引き起こす (毎回 100% diff → suspicious feed
		// 扱いで refresh が稀に / 全くされなくなる)。Redial.updated_at を使う。
		ev.SetDtStampTime(r.UpdatedAt.UTC())
		// SEQUENCE は revision counter (RFC 5545 §3.8.7.4)。
		// updated_at が変わるたびにイベントが「更新された」と認識させる必要が
		// あるが、stable な単調増加カウンタは持ってないので unix 秒で代用。
		// 同一 UID 内で過去より大きい SEQUENCE なら client は更新として扱う。
		ev.SetSequence(int(r.UpdatedAt.Unix()))
		// LAST-MODIFIED も DTSTAMP と同じ意味だが、ある client は LAST-MODIFIED
		// のみ参照する (RFC 5545 §3.8.7.3)。両方付けておく。
		ev.SetLastModifiedAt(r.UpdatedAt.UTC())
		ev.SetStartAt(r.StartAt.UTC())
		ev.SetEndAt(r.EndAt.UTC())
		ev.SetSummary(fmt.Sprintf("[Phox] %s へ掛け直し", r.CustomerName))
		ev.SetDescription(buildDescription(r, in.PhoxBaseURL))
		ev.SetStatus(ics.ObjectStatusConfirmed)
		// 検索/色分け用カテゴリ
		ev.AddProperty(ics.ComponentPropertyCategories, "Phox,Redial")
		// カレンダーから Phox のページにジャンプできるように URL も付ける
		if in.PhoxBaseURL != "" {
			ev.SetURL(customerDeepLink(in.PhoxBaseURL, r.CustomerBookID.String(), r.CustomerID.String()))
		}
	}

	var buf bytes.Buffer
	_ = cal.SerializeTo(&buf)

	// RFC 5545 §3.1 は **CRLF (\r\n)** を MUST で要求してる。arran4/golang-ical
	// v0.3.5 の SerializeTo は LF だけで書く既知のクセがあり、Outlook や
	// iOS Calendar 等の strict クライアントが「subscribe しても 0 件 / 失敗」と
	// する原因になる (Google Calendar / Apple macOS は寛容なので一見動いて見える)。
	// 全 LF を CRLF に正規化して return。
	out := bytes.ReplaceAll(buf.Bytes(), []byte("\r\n"), []byte("\n")) // collapse 既存 CRLF
	out = bytes.ReplaceAll(out, []byte("\n"), []byte("\r\n"))           // 全部 CRLF に揃える
	return out
}

func buildDescription(r db.ListRedialsByUserWithCustomerRow, phoxBaseURL string) string {
	var b bytes.Buffer
	if r.Note != "" {
		b.WriteString(r.Note)
	}
	if r.Phone != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("電話: ")
		b.WriteString(r.Phone)
	}
	if phoxBaseURL != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(customerDeepLink(phoxBaseURL, r.CustomerBookID.String(), r.CustomerID.String()))
	}
	return b.String()
}

func customerDeepLink(base, bookID, customerID string) string {
	return fmt.Sprintf("%s/book/%s/customer/%s", base, bookID, customerID)
}
