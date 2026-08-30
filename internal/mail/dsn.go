package mail

import (
	"bufio"
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"regexp"
	"strings"

	"github.com/emersion/go-message"
)

// DSNInfo は配送失敗レポート (RFC 3464 multipart/report; report-type=
// delivery-status) から抜き出したバウンス情報 (Phase 27c)。
type DSNInfo struct {
	Action            string // "failed" / "delayed" / ...
	Status            string // "5.1.1" 等。5.x.x = ハードバウンス、4.x.x = ソフト
	FinalRecipient    string // 届かなかった宛先アドレス (小文字化済み)
	OriginalMessageID string // 元メールの Message-ID (bracket 除去済み)。取れれば
}

// IsHard は恒久的な配送失敗 (5.x.x) かどうか。
func (d *DSNInfo) IsHard() bool {
	return d != nil && strings.HasPrefix(d.Status, "5")
}

var statusRe = regexp.MustCompile(`^\d\.\d{1,3}\.\d{1,3}`)

// ParseDSN は RFC822 生バイト列が DSN なら情報を抜き出して返す。DSN でなければ
// nil。非準拠 MTA も多いので、multipart/report 判定に失敗しても
// message/delivery-status パートが見つかればそれを信じる。
// パース失敗はすべて nil (= 通常メール扱い) に落とす — 取込みを止めない。
func ParseDSN(raw []byte) *DSNInfo {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	ct, params, err := entity.Header.ContentType()
	if err != nil || !strings.HasPrefix(ct, "multipart/") {
		return nil
	}
	// multipart/report; report-type=delivery-status が正規形だが、
	// report-type を落とす MTA もいるので boundary があれば中を見に行く。
	if ct == "multipart/report" && params["report-type"] != "" && params["report-type"] != "delivery-status" {
		return nil // feedback-report 等は対象外
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil
	}

	// ヘッダ部を読み飛ばして本文だけを multipart reader に渡す。
	bodyStart := bytes.Index(raw, []byte("\r\n\r\n"))
	sep := 4
	if bodyStart < 0 {
		bodyStart = bytes.Index(raw, []byte("\n\n"))
		sep = 2
	}
	if bodyStart < 0 {
		return nil
	}
	mr := multipart.NewReader(bytes.NewReader(raw[bodyStart+sep:]), boundary)

	var info *DSNInfo
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		pct, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		switch pct {
		case "message/delivery-status":
			body, rerr := io.ReadAll(io.LimitReader(part, 64*1024))
			if rerr != nil {
				continue
			}
			if parsed := parseDeliveryStatus(body); parsed != nil {
				if info == nil {
					info = parsed
				} else {
					// per-message フィールド (Original-Message-ID 等) を統合
					if info.OriginalMessageID == "" {
						info.OriginalMessageID = parsed.OriginalMessageID
					}
				}
			}
		case "message/rfc822", "text/rfc822-headers":
			// 元メールの Message-ID をヘッダから復元 (delivery-status に
			// Original-Message-ID が無い MTA 向けフォールバック)。
			body, rerr := io.ReadAll(io.LimitReader(part, 64*1024))
			if rerr != nil {
				continue
			}
			if mid := extractHeaderMessageID(body); mid != "" {
				if info == nil {
					info = &DSNInfo{}
				}
				if info.OriginalMessageID == "" {
					info.OriginalMessageID = mid
				}
			}
		}
	}
	if info == nil || (info.Status == "" && info.Action == "" && info.OriginalMessageID == "") {
		return nil
	}
	return info
}

// parseDeliveryStatus は message/delivery-status パート (ヘッダ形式の
// フィールド群が空行区切りで per-message → per-recipient と並ぶ) を解析する。
func parseDeliveryStatus(body []byte) *DSNInfo {
	info := &DSNInfo{}
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		i := strings.Index(line, ":")
		if i < 0 {
			continue
		}
		key := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(line[:i]))
		val := strings.TrimSpace(line[i+1:])
		switch key {
		case "Action":
			if info.Action == "" {
				info.Action = strings.ToLower(val)
			}
		case "Status":
			if info.Status == "" {
				if m := statusRe.FindString(val); m != "" {
					info.Status = m
				}
			}
		case "Final-Recipient":
			// "rfc822; user@example.com" 形式
			if info.FinalRecipient == "" {
				if j := strings.Index(val, ";"); j >= 0 {
					val = val[j+1:]
				}
				info.FinalRecipient = strings.ToLower(strings.TrimSpace(val))
			}
		case "Original-Message-Id":
			if info.OriginalMessageID == "" {
				info.OriginalMessageID = normalizeMessageID(val)
			}
		}
	}
	if info.Action == "" && info.Status == "" {
		return nil
	}
	return info
}

// extractHeaderMessageID は RFC822 (ヘッダのみでも可) から Message-ID を抜く。
func extractHeaderMessageID(raw []byte) string {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	return normalizeMessageID(entity.Header.Get("Message-Id"))
}

// parseReferences は References ヘッダの Message-ID 列を返す (Phase 27c)。
func parseReferences(raw []byte) []string {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	refs := entity.Header.Get("References")
	if refs == "" {
		return nil
	}
	var out []string
	for _, f := range strings.Fields(refs) {
		if n := normalizeMessageID(f); n != "" {
			out = append(out, n)
		}
	}
	return out
}
