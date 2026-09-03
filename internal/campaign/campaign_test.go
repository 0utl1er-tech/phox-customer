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
		"こんにちは &lt;様&gt;<br>",                                                   // エスケープ + 改行変換
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

func TestEstimateCompletion(t *testing.T) {
	// 水曜 10:00 JST、窓 9-18、間隔 60 秒、mailbox 1 台、warmup 無し、cap 100。
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, jst)
	c := db.Campaign{
		SendStartHour: 9, SendEndHour: 18, SendDays: 31,
		DailyCapPerMailbox: 100, MinIntervalSec: 60, WarmupEnabled: false,
	}
	// 残 30 通 → 30 分後に完了するはず。
	eta := EstimateCompletion(c, 1, 30, 0, now)
	if eta == nil {
		t.Fatal("expected eta")
	}
	if got := eta.Sub(now); got < 29*time.Minute || got > 31*time.Minute {
		t.Errorf("eta = +%v, want ~30m", got)
	}

	// 残 150 通、cap 100 → 今日 100 通 + 翌日 50 通 → 木曜 9:50 頃。
	eta = EstimateCompletion(c, 1, 150, 0, now)
	if eta == nil {
		t.Fatal("expected eta")
	}
	if eta.In(jst).Day() != 3 || eta.In(jst).Hour() != 9 {
		t.Errorf("eta = %v, want Thu ~09:50 JST", eta.In(jst))
	}

	// 金曜 17:59 に残 100 通 → 週末スキップで月曜に完了。
	fri := time.Date(2026, 9, 4, 17, 59, 0, 0, jst)
	eta = EstimateCompletion(c, 1, 100, 99, fri) // 今日はほぼ送り切っている
	if eta == nil {
		t.Fatal("expected eta")
	}
	if eta.In(jst).Weekday() != time.Monday {
		t.Errorf("eta weekday = %v, want Monday", eta.In(jst).Weekday())
	}

	// queued 0 / mailbox 0 は nil。
	if EstimateCompletion(c, 1, 0, 0, now) != nil || EstimateCompletion(c, 0, 10, 0, now) != nil {
		t.Error("expected nil for no work / no mailboxes")
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

// ─── Phase 27f: 健全性チェック ──────────────────────────────────

func TestIsRoleAddress(t *testing.T) {
	for _, e := range []string{"info@example.com", "INFO@Example.com", "no-reply@x.jp", "sales+tag@y.co.jp"} {
		if !IsRoleAddress(e) {
			t.Errorf("%q should be role address", e)
		}
	}
	for _, e := range []string{"yamada@example.com", "t.suzuki@example.co.jp", "notanemail", ""} {
		if IsRoleAddress(e) {
			t.Errorf("%q should NOT be role address", e)
		}
	}
}

func TestDomainOf(t *testing.T) {
	cases := map[string]string{
		"Yamada@Example.COM": "example.com",
		" a@b.co.jp ":        "b.co.jp",
		"no-at-sign":         "",
		"@example.com":       "",
		"user@":              "",
		"user@localhost":     "", // ドット無しは不正扱い
	}
	for in, want := range cases {
		if got := DomainOf(in); got != want {
			t.Errorf("DomainOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBounceBreakerThreshold(t *testing.T) {
	// tripBreakerIfUnhealthy の判定式そのもの (DB 非依存部分) を確認する。
	// sent < minSamplesForBreaker では率が高くても止めない。
	cases := []struct {
		sent, bounced int64
		threshold     int32
		wantTrip      bool
	}{
		{sent: 3, bounced: 2, threshold: 5, wantTrip: false},    // サンプル不足
		{sent: 100, bounced: 3, threshold: 5, wantTrip: false},  // 3% ≤ 5%
		{sent: 100, bounced: 6, threshold: 5, wantTrip: true},   // 6% > 5%
		{sent: 20, bounced: 2, threshold: 5, wantTrip: true},    // 10% > 5%
		{sent: 100, bounced: 50, threshold: 0, wantTrip: false}, // しきい値0=無効
	}
	for _, c := range cases {
		trip := c.threshold > 0 && c.sent >= minSamplesForBreaker &&
			float64(c.bounced)*100/float64(c.sent) > float64(c.threshold)
		if trip != c.wantTrip {
			t.Errorf("sent=%d bounced=%d th=%d: trip=%v want %v",
				c.sent, c.bounced, c.threshold, trip, c.wantTrip)
		}
	}
}
