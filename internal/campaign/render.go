package campaign

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
)

// jst は送信窓とプレースホルダ {{today}} の解釈に使うタイムゾーン。
var jst = time.FixedZone("Asia/Tokyo", 9*60*60)

// placeholderKeys は phox-ui の src/lib/mail-template.ts TEMPLATE_PLACEHOLDERS
// と同一セット + キャンペーン専用の {{unsubscribe_url}}。
// 未定義キーは空文字に置換し、セット外のトークンはそのまま残す (誤記の視認用)。
var placeholderKeys = []string{
	"customer_name",
	"customer_corporation",
	"customer_mail",
	"customer_phone",
	"sender_name",
	"sender_mail",
	"today",
	"unsubscribe_url",
}

var placeholderRegexps = func() map[string]*regexp.Regexp {
	m := make(map[string]*regexp.Regexp, len(placeholderKeys))
	for _, k := range placeholderKeys {
		m[k] = regexp.MustCompile(`\{\{\s*` + k + `\s*\}\}`)
	}
	return m
}()

// Render はテンプレートを vars で置換する (mail-template.ts と同セマンティクス)。
func Render(tpl string, vars map[string]string) string {
	out := tpl
	for _, k := range placeholderKeys {
		out = placeholderRegexps[k].ReplaceAllString(out, vars[k])
	}
	return out
}

// SenderInfo は特定電子メール法の送信者表示。
type SenderInfo struct {
	Org     string // 会社名/氏名
	Address string // 住所
	Contact string // 電話 or 問い合わせ先
}

// RenderBody は本文をレンダリングし、特電法フッター (送信者表示 + 配信停止
// リンク) を必ず末尾に付ける。本文中に {{unsubscribe_url}} があればそこにも
// 展開されるが、フッターの記載は省略できない (法令上の表示義務)。
func RenderBody(tpl string, vars map[string]string, sender SenderInfo, unsubscribeURL string) string {
	vars["unsubscribe_url"] = unsubscribeURL
	body := Render(tpl, vars)
	return body + buildFooter(sender, unsubscribeURL)
}

func buildFooter(sender SenderInfo, unsubscribeURL string) string {
	var b strings.Builder
	b.WriteString("\n\n------------------------------------------------------------\n")
	if sender.Org != "" {
		b.WriteString(fmt.Sprintf("送信者: %s\n", sender.Org))
	}
	if sender.Address != "" {
		b.WriteString(fmt.Sprintf("所在地: %s\n", sender.Address))
	}
	if sender.Contact != "" {
		b.WriteString(fmt.Sprintf("連絡先: %s\n", sender.Contact))
	}
	b.WriteString("今後このようなご案内が不要な場合は、以下の URL から配信停止いただけます。\n")
	b.WriteString(unsubscribeURL + "\n")
	return b.String()
}

// TodayJST returns "YYYY-MM-DD" (placeholder {{today}}).
func TodayJST(now time.Time) string {
	return now.In(jst).Format("2006-01-02")
}

// ─── Phase 27b: クリック/開封トラッキング用の本文加工 ────────────────

// urlRegex は本文中の裸 URL。末尾の句読点/閉じ括弧は URL に含めない。
var urlRegex = regexp.MustCompile(`https?://[^\s<>"'）」』]+`)

// ExtractLinks は本文中の URL を出現順・重複排除で返す。
func ExtractLinks(body string) []string {
	seen := map[string]bool{}
	var out []string
	for _, u := range urlRegex.FindAllString(body, -1) {
		u = strings.TrimRight(u, ".,;:!?)")
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// BuildHTMLBody は text/plain 本文から HTML alt パートを組み立てる。
//   - HTML エスケープ + 改行 → <br>
//   - URL は linkURL が返すトラッキング URL の <a> に置換 (ok=false ならそのまま)
//   - pixelURL 非空なら末尾に 1×1 開封ピクセルを付ける
//
// text パートの URL は書き換えない — マルチパート対応クライアントは HTML を
// 表示するのでクリック計測はそちらで取り、text は可読性を優先する。
func BuildHTMLBody(textBody string, linkURL func(url string) (string, bool), pixelURL string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><body style="font-family:sans-serif;font-size:14px;line-height:1.7;color:#222;">` + "\n<p>")
	rest := textBody
	for len(rest) > 0 {
		loc := urlRegex.FindStringIndex(rest)
		if loc == nil {
			b.WriteString(escapeText(rest))
			break
		}
		b.WriteString(escapeText(rest[:loc[0]]))
		raw := strings.TrimRight(rest[loc[0]:loc[1]], ".,;:!?)")
		trail := rest[loc[0]+len(raw) : loc[1]]
		href := raw
		if linkURL != nil {
			if t, ok := linkURL(raw); ok {
				href = t
			}
		}
		b.WriteString(`<a href="` + html.EscapeString(href) + `">` + html.EscapeString(raw) + `</a>`)
		b.WriteString(escapeText(trail))
		rest = rest[loc[1]:]
	}
	b.WriteString("</p>\n")
	if pixelURL != "" {
		b.WriteString(`<img src="` + html.EscapeString(pixelURL) + `" width="1" height="1" alt="" style="display:none;">` + "\n")
	}
	b.WriteString("</body></html>")
	return b.String()
}

func escapeText(s string) string {
	return strings.ReplaceAll(html.EscapeString(s), "\n", "<br>\n")
}
