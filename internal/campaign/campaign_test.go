package campaign

import (
	"strings"
	"testing"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestTokenRoundTrip(t *testing.T) {
	tz := NewTokenizer([]byte("0123456789abcdef0123456789abcdef"))
	id := uuid.New()
	tok := tz.Token(KindUnsubscribe, id, 42)

	kind, gotID, idx, err := tz.Parse(tok)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if kind != KindUnsubscribe || gotID != id || idx != 42 {
		t.Fatalf("round trip mismatch: kind=%c id=%s idx=%d", kind, gotID, idx)
	}
}

func TestTokenRejectsTampering(t *testing.T) {
	tz := NewTokenizer([]byte("0123456789abcdef0123456789abcdef"))
	tok := tz.Token(KindUnsubscribe, uuid.New(), 0)

	// 署名部を壊す
	broken := tok[:len(tok)-2] + "AA"
	if _, _, _, err := tz.Parse(broken); err == nil {
		t.Fatal("expected error for tampered token")
	}
	// 別鍵で発行したトークンは弾く
	other := NewTokenizer([]byte("ffffffffffffffffffffffffffffffff"))
	if _, _, _, err := tz.Parse(other.Token(KindOpen, uuid.New(), 0)); err == nil {
		t.Fatal("expected error for foreign-key token")
	}
	if _, _, _, err := tz.Parse("not-base64!!"); err == nil {
		t.Fatal("expected error for garbage token")
	}
}

func TestRenderPlaceholders(t *testing.T) {
	got := Render("{{customer_name}} 様 ({{ customer_corporation }}) {{unknown}}", map[string]string{
		"customer_name":        "山田",
		"customer_corporation": "株式会社A",
	})
	want := "山田 様 (株式会社A) {{unknown}}"
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}

func TestRenderBodyAppendsFooter(t *testing.T) {
	body := RenderBody("こんにちは {{customer_name}} 様", map[string]string{"customer_name": "山田"},
		SenderInfo{Org: "株式会社アウトライアー", Address: "東京都...", Contact: "03-1234-5678"},
		"https://phox-api.example/u/abc")

	for _, want := range []string{
		"こんにちは 山田 様",
		"株式会社アウトライアー",
		"東京都...",
		"03-1234-5678",
		"配信停止",
		"https://phox-api.example/u/abc",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

func TestRenderBodyInlineUnsubscribePlaceholder(t *testing.T) {
	body := RenderBody("停止は {{unsubscribe_url}} から", map[string]string{},
		SenderInfo{Org: "X"}, "https://u.example/t")
	if !strings.HasPrefix(body, "停止は https://u.example/t から") {
		t.Fatalf("inline placeholder not replaced: %s", body)
	}
}

func TestExtractLinks(t *testing.T) {
	body := "詳細は https://example.com/a をご覧ください。\nhttps://example.com/a と https://example.com/b?x=1 も。"
	got := ExtractLinks(body)
	want := []string{"https://example.com/a", "https://example.com/b?x=1"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ExtractLinks = %v, want %v", got, want)
	}
}

func TestBuildHTMLBody(t *testing.T) {
	html := BuildHTMLBody("こんにちは <様>\nhttps://example.com/page を見てください",
		func(u string) (string, bool) {
			if u == "https://example.com/page" {
				return "https://api.example/t/c/tok123", true
			}
			return "", false
		},
		"https://api.example/t/o/pix456")

	for _, want := range []string{
		"こんにちは &lt;様&gt;<br>",                                                // エスケープ + 改行変換
		`<a href="https://api.example/t/c/tok123">https://example.com/page</a>`, // リンク書き換え (表示は元URL)
		`<img src="https://api.example/t/o/pix456"`,                             // 開封ピクセル
	} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, `href="https://example.com/page"`) {
		t.Error("original URL should not remain as href when rewritten")
	}
}

func TestBuildHTMLBodyNoTracking(t *testing.T) {
	html := BuildHTMLBody("プレーン https://example.com", nil, "")
	if !strings.Contains(html, `<a href="https://example.com">`) {
		t.Errorf("untracked link should keep original href:\n%s", html)
	}
	if strings.Contains(html, "<img") {
		t.Error("no pixel expected")
	}
}

func mkCampaign(start, end, days int32) db.Campaign {
	return db.Campaign{SendStartHour: start, SendEndHour: end, SendDays: days}
}

func TestInSendWindow(t *testing.T) {
	// 2026-09-02 は水曜。JST 10:00。
	wedMorning := time.Date(2026, 9, 2, 10, 0, 0, 0, jst)
	if !inSendWindow(mkCampaign(9, 18, 0), wedMorning) {
		t.Error("weekday business hours should be in window (send_days=0 → 平日)")
	}
	if inSendWindow(mkCampaign(9, 18, 0), time.Date(2026, 9, 2, 18, 0, 0, 0, jst)) {
		t.Error("18:00 should be outside [9,18)")
	}
	// 2026-09-06 は日曜。平日 bitmask では送らない。
	sun := time.Date(2026, 9, 6, 10, 0, 0, 0, jst)
	if inSendWindow(mkCampaign(9, 18, 31), sun) {
		t.Error("sunday should be excluded with weekday mask")
	}
	if !inSendWindow(mkCampaign(9, 18, 64), sun) {
		t.Error("sunday should be included with Sun bit")
	}
}

func TestEffectiveCapWarmup(t *testing.T) {
	w := &Worker{}
	now := time.Now()
	c := db.Campaign{
		DailyCapPerMailbox: 100,
		WarmupEnabled:      true,
		StartedAt:          pgtype.Timestamptz{Time: now.Add(-49 * time.Hour), Valid: true}, // 2 日経過
	}
	if got := w.effectiveCap(c, now); got != 40 { // 20 + 10*2
		t.Errorf("warmup cap = %d, want 40", got)
	}
	c.StartedAt.Time = now.Add(-30 * 24 * time.Hour)
	if got := w.effectiveCap(c, now); got != 100 {
		t.Errorf("ramp should clamp at cap, got %d", got)
	}
	c.WarmupEnabled = false
	if got := w.effectiveCap(c, now); got != 100 {
		t.Errorf("warmup off should return cap, got %d", got)
	}
}
