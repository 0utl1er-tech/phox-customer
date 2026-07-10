package mail

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseBodyAndAttachments_PlainWithAttachment(t *testing.T) {
	raw := strings.Join([]string{
		"From: sender@example.com",
		"To: mb@0utl1er.tech",
		"Subject: test",
		"MIME-Version: 1.0",
		`Content-Type: multipart/mixed; boundary="BOUNDARY"`,
		"",
		"--BOUNDARY",
		`Content-Type: text/plain; charset="utf-8"`,
		"",
		"こんにちは。本文です。",
		"--BOUNDARY",
		"Content-Type: application/pdf",
		`Content-Disposition: attachment; filename="estimate.pdf"`,
		"",
		"%PDF-fake",
		"--BOUNDARY--",
		"",
	}, "\r\n")

	body, atts := parseBodyAndAttachments([]byte(raw))
	assert.Contains(t, body, "こんにちは。本文です。")
	assert.Equal(t, []string{"estimate.pdf"}, atts)
}

func TestParseBodyAndAttachments_HTMLFallback(t *testing.T) {
	raw := strings.Join([]string{
		"From: sender@example.com",
		"Subject: html only",
		"MIME-Version: 1.0",
		`Content-Type: text/html; charset="utf-8"`,
		"",
		"<html><body><p>お世話になります。</p><br><div>HTML だけのメールです&amp;テスト</div></body></html>",
		"",
	}, "\r\n")

	body, atts := parseBodyAndAttachments([]byte(raw))
	assert.Contains(t, body, "お世話になります。")
	assert.Contains(t, body, "HTML だけのメールです&テスト")
	assert.NotContains(t, body, "<p>", "タグは除去される")
	assert.Empty(t, atts)
}

func TestParseBodyAndAttachments_Garbage(t *testing.T) {
	body, atts := parseBodyAndAttachments([]byte("not an email at all"))
	// パース失敗でも panic せず空で返る (取込みを止めない)。
	assert.Empty(t, atts)
	_ = body
}
