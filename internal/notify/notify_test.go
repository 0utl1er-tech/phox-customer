package notify

import "testing"

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
