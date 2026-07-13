package mail

import (
	"io"
	"regexp"
	"strings"

	gomail "github.com/emersion/go-message/mail"
)

// bodyMaxBytes は保存する本文の上限。巨大メールで DB を膨らませない。
const bodyMaxBytes = 256 * 1024

var htmlTagRe = regexp.MustCompile(`(?s)<[^>]*>`)

// parseBodyAndAttachments は RFC822 生バイト列から text/plain 本文と
// 添付ファイル名を取り出す。text/plain が無ければ text/html のタグを
// 除去したものを本文とする。パース失敗時は空を返す (取込み自体は止めない)。
func parseBodyAndAttachments(raw []byte) (string, []string) {
	mr, err := gomail.CreateReader(strings.NewReader(string(raw)))
	if err != nil {
		return "", nil
	}
	defer mr.Close()

	var plain, html string
	var attachments []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break // 壊れた part 以降は諦める (取れた分は返す)
		}
		switch h := p.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ := h.ContentType()
			b, rerr := io.ReadAll(io.LimitReader(p.Body, bodyMaxBytes))
			if rerr != nil {
				continue
			}
			switch {
			case strings.HasPrefix(ct, "text/plain") && plain == "":
				plain = string(b)
			case strings.HasPrefix(ct, "text/html") && html == "":
				html = string(b)
			}
		case *gomail.AttachmentHeader:
			if name, nerr := h.Filename(); nerr == nil && name != "" {
				attachments = append(attachments, name)
			}
		}
	}

	body := plain
	if body == "" && html != "" {
		body = stripHTML(html)
	}
	return strings.TrimSpace(body), attachments
}

// stripHTML は雑に HTML タグを除去してテキスト化する。表示ではなく
// 「Claude/人が内容を読む」用途なので厳密なレンダリングは不要。
func stripHTML(s string) string {
	// <br> / </p> は改行として残す
	s = regexp.MustCompile(`(?i)<br\s*/?>|</p>`).ReplaceAllString(s, "\n")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	// 連続空行を圧縮
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
	return s
}
