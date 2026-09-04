package customfields

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"meo_score", "meo_score", true},
		{"MEO Score", "meo_score", true},
		{"  Web-Site  ", "web_site", true},
		{"score(100)", "score_100_", true},
		{"a1_b2", "a1_b2", true},
		{"", "", false},
		{"   ", "", false},
		// 有効な文字が 1 つも無い列名 (日本語のみ・記号のみ) は
		// '_' の羅列にしかならず、テンプレートから参照できないので取らない。
		{"店舗メモ", "", false},
		{"---", "", false},
		{"__", "", false},
	}
	for _, tc := range cases {
		got, ok := NormalizeKey(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("NormalizeKey(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNormalizeKeyTruncates(t *testing.T) {
	got, ok := NormalizeKey(strings.Repeat("a", MaxKeyLen+50))
	if !ok || len(got) != MaxKeyLen {
		t.Errorf("NormalizeKey long = (%d chars, %v), want (%d, true)", len(got), ok, MaxKeyLen)
	}
}

func TestValidKey(t *testing.T) {
	for _, k := range []string{"a", "meo_score", "a1_b2", strings.Repeat("a", MaxKeyLen)} {
		if !ValidKey(k) {
			t.Errorf("ValidKey(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"", "MEO", "meo-score", "meo score", "店舗", strings.Repeat("a", MaxKeyLen+1)} {
		if ValidKey(k) {
			t.Errorf("ValidKey(%q) = true, want false", k)
		}
	}
}

func TestTruncateValueKeepsUTF8(t *testing.T) {
	// 3 バイト文字を上限ちょうど超えるまで並べる。
	v := strings.Repeat("あ", MaxValueLen)
	got := TruncateValue(v)
	if len(got) > MaxValueLen {
		t.Errorf("TruncateValue len = %d, want <= %d", len(got), MaxValueLen)
	}
	if !utf8.ValidString(got) {
		t.Error("TruncateValue broke a UTF-8 sequence")
	}
	// 上限以下はそのまま
	if got := TruncateValue("短い"); got != "短い" {
		t.Errorf("TruncateValue short = %q", got)
	}
}

func TestSanitize(t *testing.T) {
	in := map[string]string{
		"MEO Score":  "30/100",
		"店舗メモ":      "落ちる",
		"meo_issues": "・写真が3枚以下\n・投稿停止",
	}
	got := Sanitize(in)
	if len(got) != 2 {
		t.Fatalf("Sanitize = %#v, want 2 keys", got)
	}
	if got["meo_score"] != "30/100" {
		t.Errorf("meo_score = %q", got["meo_score"])
	}
	// 改行はそのまま残す (箇条書きの診断結果が主用途)。
	if got["meo_issues"] != "・写真が3枚以下\n・投稿停止" {
		t.Errorf("改行が失われた: %q", got["meo_issues"])
	}
}

func TestSanitizeEnforcesLimits(t *testing.T) {
	in := map[string]string{}
	for i := 0; i < MaxFields+20; i++ {
		in[string(rune('a'+i%26))+strings.Repeat("x", i)] = "v"
	}
	in["big"] = strings.Repeat("z", MaxValueLen+100)
	got := Sanitize(in)
	if len(got) != MaxFields {
		t.Errorf("Sanitize len = %d, want %d", len(got), MaxFields)
	}
	if v, ok := got["big"]; ok && len(v) > MaxValueLen {
		t.Errorf("big len = %d, want <= %d", len(v), MaxValueLen)
	}
}

// 件数超過の切り捨ては map の反復順に依存せず決定的であること
// (同じ入力で毎回同じキーが残る)。
func TestSanitizeIsDeterministic(t *testing.T) {
	in := map[string]string{}
	for i := 0; i < MaxFields+10; i++ {
		in[strings.Repeat("a", i+1)] = "v"
	}
	first := Sanitize(in)
	for i := 0; i < 20; i++ {
		got := Sanitize(in)
		if len(got) != len(first) {
			t.Fatalf("len mismatch")
		}
		for k := range first {
			if _, ok := got[k]; !ok {
				t.Fatalf("キー %q が run ごとにブレる", k)
			}
		}
	}
}

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	b, err := Marshal(nil)
	if err != nil || string(b) != "{}" {
		t.Fatalf("Marshal(nil) = %q, %v; want {}", b, err)
	}
	in := map[string]string{"meo_score": "30/100", "meo_issues": "a\nb"}
	b, err = Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	got := Unmarshal(b)
	if got["meo_score"] != "30/100" || got["meo_issues"] != "a\nb" {
		t.Errorf("round trip = %#v", got)
	}
}

// 壊れた/想定外の JSON は「差し込み変数なし」に倒す — 送信経路を
// JSON の不整合で止めないため。
func TestUnmarshalDegradesGracefully(t *testing.T) {
	for _, raw := range []string{"", "null", "not json", `[1,2]`, `{"a": 1}`, `{"a": {"b":"c"}}`} {
		if got := Unmarshal([]byte(raw)); len(got) != 0 {
			t.Errorf("Unmarshal(%q) = %#v, want empty", raw, got)
		}
	}
}
