package mail

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// IMAPTLSMode はサーバー接続の TLS 方式。
//   - "implicit"  (= IMAPS, port 993) — mailu の本番想定
//   - "starttls"  (port 143 → STARTTLS 昇格) — 汎用
//   - "none"      (port 143 平文)       — テスト / in-memory サーバー用
type IMAPTLSMode string

const (
	IMAPTLSImplicit IMAPTLSMode = "implicit"
	IMAPTLSStartTLS IMAPTLSMode = "starttls"
	IMAPTLSNone     IMAPTLSMode = "none"
)

// IMAPConnectConfig は IMAP 接続に必要な値の一式。
type IMAPConnectConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	TLSMode  IMAPTLSMode
	// TLSInsecureSkipVerify は LAN 内自己署名証明書を許容する時だけ true に
	// する。dev/staging 以外で true にしないこと。
	TLSInsecureSkipVerify bool
}

func (c IMAPConnectConfig) addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// ParsedMessage は IMAP から取得したメッセージのうち、phox が Activity として
// 取込むのに必要な最小限のフィールド。
type ParsedMessage struct {
	UID       imap.UID
	MessageID string   // RFC822 Message-ID (`<xxx@yyy>` 形式)。dedup キー。
	Date      time.Time
	Subject   string
	From      string   // 最初の差出人アドレス (email 部分のみ)
	To        []string // 全宛先アドレス (email 部分のみ)
	Cc        []string
	// Body の取得はコストが高いので Phase 14 では取らない (message_id で dedup
	// されるので、後から別途 fetch する拡張も可能)。
}

// IMAPClient は imapclient.Client を phox 用途にラップする。
// Login + Select 済み mailbox で FetchSince を呼ぶ前提。
type IMAPClient struct {
	c *imapclient.Client
}

// DialIMAP は config に従って IMAP サーバーに接続 + LOGIN する。
func DialIMAP(cfg IMAPConnectConfig) (*IMAPClient, error) {
	if cfg.Host == "" {
		return nil, errors.New("imap: host required")
	}
	if cfg.Port == 0 {
		// mode デフォルト
		switch cfg.TLSMode {
		case IMAPTLSImplicit, "":
			cfg.Port = 993
		default:
			cfg.Port = 143
		}
	}

	var (
		c   *imapclient.Client
		err error
	)

	tlsCfg := &tls.Config{
		ServerName:         cfg.Host,
		InsecureSkipVerify: cfg.TLSInsecureSkipVerify,
	}
	opts := &imapclient.Options{
		TLSConfig: tlsCfg,
	}

	switch cfg.TLSMode {
	case IMAPTLSImplicit, "":
		c, err = imapclient.DialTLS(cfg.addr(), opts)
	case IMAPTLSStartTLS:
		c, err = imapclient.DialStartTLS(cfg.addr(), opts)
	case IMAPTLSNone:
		c, err = imapclient.DialInsecure(cfg.addr(), nil)
	default:
		return nil, fmt.Errorf("imap: unknown TLS mode %q", cfg.TLSMode)
	}
	if err != nil {
		return nil, fmt.Errorf("imap: dial: %w", err)
	}

	if err := c.Login(cfg.Username, cfg.Password).Wait(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("imap: login: %w", err)
	}

	return &IMAPClient{c: c}, nil
}

// Close は LOGOUT してコネクションを閉じる。
func (ic *IMAPClient) Close() error {
	if ic == nil || ic.c == nil {
		return nil
	}
	_ = ic.c.Logout().Wait()
	return ic.c.Close()
}

// FetchSince は指定 mailbox を SELECT し、`since` 以降の日付のメッセージを
// UID SEARCH → FETCH (Envelope + UID) して ParsedMessage の slice を返す。
// Body までは取らず、envelope 情報だけで十分 (Activity 生成用)。
//
// Note: IMAP の SEARCH SINCE は「日付単位」で比較するので、分単位で絞りたい
// 場合でも当日の 00:00 まで遡ることになる。phox 側で dedup されるので実害なし。
func (ic *IMAPClient) FetchSince(mailbox string, since time.Time) ([]ParsedMessage, error) {
	if ic == nil || ic.c == nil {
		return nil, errors.New("imap: client not connected")
	}

	if _, err := ic.c.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return nil, fmt.Errorf("imap: select %q: %w", mailbox, err)
	}

	searchCriteria := &imap.SearchCriteria{}
	if !since.IsZero() {
		searchCriteria.Since = since
	}
	searchData, err := ic.c.UIDSearch(searchCriteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap: search: %w", err)
	}
	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}

	uidSet := imap.UIDSet{}
	for _, u := range uids {
		uidSet.AddNum(u)
	}
	fetchCmd := ic.c.Fetch(uidSet, &imap.FetchOptions{
		Envelope: true,
		UID:      true,
	})
	defer fetchCmd.Close()

	msgs := make([]ParsedMessage, 0, len(uids))
	for {
		m := fetchCmd.Next()
		if m == nil {
			break
		}
		buf, err := m.Collect()
		if err != nil {
			return nil, fmt.Errorf("imap: collect: %w", err)
		}
		if buf.Envelope == nil {
			continue
		}
		pm := ParsedMessage{
			UID:       buf.UID,
			MessageID: normalizeMessageID(buf.Envelope.MessageID),
			Date:      buf.Envelope.Date,
			Subject:   buf.Envelope.Subject,
		}
		if len(buf.Envelope.From) > 0 {
			pm.From = buf.Envelope.From[0].Addr()
		}
		for _, a := range buf.Envelope.To {
			if addr := a.Addr(); addr != "" {
				pm.To = append(pm.To, addr)
			}
		}
		for _, a := range buf.Envelope.Cc {
			if addr := a.Addr(); addr != "" {
				pm.Cc = append(pm.Cc, addr)
			}
		}
		msgs = append(msgs, pm)
	}
	return msgs, nil
}

// normalizeMessageID は `<abc@host>` 形式の angle brackets を剥がす。
// phox 側 DB の `message_id` カラムも bracket 無し形式で統一する。
func normalizeMessageID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	return s
}
