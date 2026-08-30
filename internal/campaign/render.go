package campaign

import (
	"fmt"
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
