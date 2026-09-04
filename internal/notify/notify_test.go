package notify

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestValidateWebhookURL(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"", true}, // 空 = 通知無効
		{"https://discord.com/api/webhooks/123/abc", true},
		{"http://notify-echo.phox.svc/hook", true}, // staging 検証用 (cluster 内)
		{"ftp://example.com/x", false},
		{"discord.com/api/webhooks/123", false}, // スキーム無し
		{"https://", false},                     // ホスト無し
		{"not a url", false},
	}
	for _, c := range cases {
		err := ValidateWebhookURL(c.url)
		if (err == nil) != c.ok {
			t.Errorf("ValidateWebhookURL(%q) = %v, want ok=%v", c.url, err, c.ok)
		}
	}
}

func TestNormalizeEvents(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"reply", "reply", false},
		{"click,reply", "reply,click", false},            // canonical 順に直る
		{" reply , open ", "reply,open", false},          // trim
		{"reply,reply", "reply", false},                  // 重複除去
		{"reply,click,unsubscribe,bounce,open", "reply,click,unsubscribe,bounce,open", false},
		// Phase 28f: 自動下書き通知 (オプトイン — 既定値には入っていない)。
		{"autodraft", "autodraft", false},
		{"autodraft,reply", "reply,autodraft", false},
		{"reply,unknown", "", true},
		{"REPLY", "", true}, // 大文字は不許可 (既知値のみ)
	}
	for _, c := range cases {
		got, err := NormalizeEvents(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeEvents(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeEvents(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeEvents(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEventEnabled(t *testing.T) {
	cases := []struct {
		csv  string
		kind string
		want bool
	}{
		{"reply", "reply", true},
		{"reply", "click", false},
		{"reply,click,open", "open", true},
		{"", "reply", false},
		{" reply , click ", "click", true}, // 手書き設定の空白にも耐える
		{"replying", "reply", false},       // 部分一致はしない
	}
	for _, c := range cases {
		if got := EventEnabled(c.csv, c.kind); got != c.want {
			t.Errorf("EventEnabled(%q, %q) = %v, want %v", c.csv, c.kind, got, c.want)
		}
	}
}

// Phase 28f: 自動下書き通知の embed 内容。Discord に出す文面が
// 「開始は人間」を明示していること、件数が出ていることを固定する。
func TestBuildAutoDraftPayload(t *testing.T) {
	n := NewDiscordNotifier(nil, "https://phox.example.com/")
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	got := n.buildAutoDraftPayload(AutoDraftInfo{
		CampaignID:     id,
		CampaignName:   "GM_整体院_埼玉県_2026-09_HPなし",
		BookName:       "GM_整体院_埼玉県_2026-09_HPなし",
		TemplateName:   "HPなし: HP制作の新規提案",
		RecipientCount: 2,
		SkippedCount:   1,
	})
	if len(got.Embeds) != 1 {
		t.Fatalf("embeds = %d, want 1", len(got.Embeds))
	}
	e := got.Embeds[0]
	if want := "https://phox.example.com/campaigns/" + id.String(); e.URL != want {
		t.Errorf("URL = %q, want %q", e.URL, want)
	}
	joined := ""
	for _, f := range e.Fields {
		joined += f.Name + "=" + f.Value + "\n"
	}
	for _, want := range []string{
		"GM_整体院_埼玉県_2026-09_HPなし",
		"HPなし: HP制作の新規提案",
		"2 件 (除外 1 件)",
		"自動では送信しません",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("fields %q に %q が含まれない", joined, want)
		}
	}
}
