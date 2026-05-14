package ical_test

import (
	"strings"
	"testing"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/ical"
	"github.com/google/uuid"
)

func fixedTime(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tt
}

func sampleRows(t *testing.T) []db.ListRedialsByUserWithCustomerRow {
	t.Helper()
	return []db.ListRedialsByUserWithCustomerRow{
		{
			ID:             uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			CustomerID:     uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			CustomerBookID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			CustomerName:   "田中太郎",
			UserID:         "user-1",
			Phone:          "03-1234-5678",
			Note:           "初回挨拶のフォロー",
			StartAt:        fixedTime(t, "2026-05-01T05:30:00Z"),
			EndAt:          fixedTime(t, "2026-05-01T06:00:00Z"),
		},
		{
			ID:             uuid.MustParse("44444444-4444-4444-4444-444444444444"),
			CustomerID:     uuid.MustParse("55555555-5555-5555-5555-555555555555"),
			CustomerBookID: uuid.MustParse("66666666-6666-6666-6666-666666666666"),
			CustomerName:   "山田花子",
			UserID:         "user-1",
			Phone:          "",
			Note:           "",
			StartAt:        fixedTime(t, "2026-05-02T01:00:00Z"),
			EndAt:          fixedTime(t, "2026-05-02T01:30:00Z"),
		},
	}
}

func TestBuildFeed_VEventCountAndHeaders(t *testing.T) {
	out := ical.BuildFeed(ical.FeedInput{
		UserID:      "user-1",
		UserName:    "テスト ユーザー",
		PhoxBaseURL: "http://localhost:3000",
		Redials:     sampleRows(t),
		GeneratedAt: fixedTime(t, "2026-04-11T12:00:00Z"),
	})
	// RFC 5545 line folding を解除してから substring 判定する。
	// long line は "\r\n " でぶつ切りされるので単純 replace で復元可能。
	s := unfold(string(out))

	// 1. Calendar 全体のプロパティ
	mustContain(t, s, "BEGIN:VCALENDAR")
	mustContain(t, s, "END:VCALENDAR")
	mustContain(t, s, "PRODID:-//Phox//Phox CRM iCal Feed 1.0//EN")
	mustContain(t, s, "VERSION:2.0")
	mustContain(t, s, "METHOD:PUBLISH")
	mustContain(t, s, "X-WR-TIMEZONE:Asia/Tokyo")
	// Japanese 文字は ics library が quoted-printable 化しないのでそのまま含まれる
	mustContain(t, s, "Phox — テスト ユーザー の掛け直し予定")

	// 2. VEVENT 数 = 2
	if got := strings.Count(s, "BEGIN:VEVENT"); got != 2 {
		t.Fatalf("VEVENT count: got %d want 2", got)
	}
	if got := strings.Count(s, "END:VEVENT"); got != 2 {
		t.Fatalf("END:VEVENT count: got %d want 2", got)
	}

	// 3. 各 event に安定した UID と SUMMARY
	mustContain(t, s, "UID:phox-redial-11111111-1111-1111-1111-111111111111@phox.local")
	mustContain(t, s, "UID:phox-redial-44444444-4444-4444-4444-444444444444@phox.local")
	mustContain(t, s, "SUMMARY:[Phox] 田中太郎 へ掛け直し")
	mustContain(t, s, "SUMMARY:[Phox] 山田花子 へ掛け直し")

	// 4. DTSTART/DTEND は UTC Z 形式 (TZID なし)
	mustContain(t, s, "DTSTART:20260501T053000Z")
	mustContain(t, s, "DTEND:20260501T060000Z")
	if strings.Contains(s, "TZID=") {
		t.Error("feed must not contain TZID (we use UTC Z format)")
	}

	// 5. STATUS: CONFIRMED
	if got := strings.Count(s, "STATUS:CONFIRMED"); got != 2 {
		t.Errorf("STATUS:CONFIRMED count: got %d want 2", got)
	}

	// 6. Deep link (URL + DESCRIPTION に含まれる)
	mustContain(t, s, "http://localhost:3000/book/33333333-3333-3333-3333-333333333333/customer/22222222-2222-2222-2222-222222222222")
}

// TestBuildFeed_CRLFLineEndings は RFC 5545 §3.1 が MUST で要求する
// CRLF 改行が全行に入っているかを検証する。arran4/golang-ical の SerializeTo
// が LF だけ書いてしまうクセに対する post-processing が効いてること、と
// Outlook/iOS の strict client で subscribe が成立する保証。
func TestBuildFeed_CRLFLineEndings(t *testing.T) {
	out := ical.BuildFeed(ical.FeedInput{
		UserID:      "user-1",
		UserName:    "テスト",
		PhoxBaseURL: "http://localhost:3000",
		Redials:     nil,
		GeneratedAt: time.Now(),
	})
	s := string(out)
	if strings.Contains(s, "\n") && !strings.Contains(s, "\r\n") {
		t.Fatal("output has bare LF but no CRLF — RFC 5545 violation")
	}
	// 末尾以外で bare LF (= \r\n でない \n) が無いことを確認
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '\n' && (i == 0 || s[i-1] != '\r') {
			t.Fatalf("bare LF at offset %d (no preceding CR)", i)
		}
	}
	if !strings.HasSuffix(s, "END:VCALENDAR\r\n") && !strings.HasSuffix(s, "END:VCALENDAR") {
		t.Fatalf("calendar does not end cleanly: %q", s[len(s)-30:])
	}
}

func TestBuildFeed_EmptyRedials(t *testing.T) {
	out := ical.BuildFeed(ical.FeedInput{
		UserID:      "user-1",
		UserName:    "Empty",
		PhoxBaseURL: "http://localhost:3000",
		Redials:     nil,
		GeneratedAt: time.Now(),
	})
	s := unfold(string(out))
	mustContain(t, s, "BEGIN:VCALENDAR")
	mustContain(t, s, "END:VCALENDAR")
	if strings.Contains(s, "BEGIN:VEVENT") {
		t.Error("expected no VEVENT for empty input")
	}
}

func TestBuildFeed_DescriptionIncludesNoteAndPhone(t *testing.T) {
	out := ical.BuildFeed(ical.FeedInput{
		UserID:      "user-1",
		UserName:    "U",
		PhoxBaseURL: "http://localhost:3000",
		Redials:     sampleRows(t)[:1], // 最初の行だけ
		GeneratedAt: fixedTime(t, "2026-04-11T12:00:00Z"),
	})
	s := unfold(string(out))
	// DESCRIPTION の中身 (ics の場合、内容は行 folding される可能性があるので
	// 部分一致で厳密にチェックしない。キーワードで押さえる)
	if !strings.Contains(s, "初回挨拶") {
		t.Error("description should include note")
	}
	if !strings.Contains(s, "03-1234-5678") {
		t.Error("description should include phone")
	}
}

func mustContain(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Errorf("output missing %q\n---\n%s\n---", want, s)
	}
}

// unfold は RFC 5545 の line folding を解除する。
// 75 char 超のプロパティは "\r\n " (or "\r\n\t") で分割されるため、
// これを除去して 1 本の論理行に戻す。
func unfold(s string) string {
	s = strings.ReplaceAll(s, "\r\n ", "")
	s = strings.ReplaceAll(s, "\r\n\t", "")
	s = strings.ReplaceAll(s, "\n ", "")
	s = strings.ReplaceAll(s, "\n\t", "")
	return s
}
