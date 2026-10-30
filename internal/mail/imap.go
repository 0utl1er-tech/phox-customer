package mail

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/rs/zerolog/log"
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
	MessageID string // RFC822 Message-ID (`<xxx@yyy>` 形式)。dedup キー。
	Date      time.Time
	Subject   string
	From      string   // 最初の差出人アドレス (email 部分のみ)
	To        []string // 全宛先アドレス (email 部分のみ)
	Cc        []string
	// Body/AttachmentNames は FetchSinceFull でのみ埋まる (FetchSince は
	// envelope のみで軽量なまま — Phase 14 の設計を維持)。
	Body            string   // text/plain (無ければ HTML のタグ除去)
	AttachmentNames []string // 添付ファイル名 (中身は取らない)

	// Phase 27c: キャンペーン返信/バウンス紐付け用。
	// InReplyTo は envelope 由来 (FetchSince でも埋まる)、References と DSN は
	// 生ヘッダ解析が必要なので FetchSinceFull でのみ埋まる。
	InReplyTo  []string // 返信元 Message-ID (bracket 除去済み)
	References []string // References ヘッダの Message-ID 列 (bracket 除去済み)
	DSN        *DSNInfo // 配送失敗レポート (DSN) なら non-nil
}

// IMAPClient は imapclient.Client を phox 用途にラップする。
// Login + Select 済み mailbox で FetchSince を呼ぶ前提。
type IMAPClient struct {
	c        *imapclient.Client
	cfg      IMAPConnectConfig // 再接続用
	username string
	cursor   *UIDCursor // nil なら毎回 INTERNALDATE で絞る (状態なし)
	selected string     // 現在 SELECT 中の mailbox (再接続時に SELECT し直す)
}

// UIDCursor は (アカウント, フォルダ) ごとに「前回 SELECT 時の UIDNEXT」を
// 覚える。2 回目以降の取込みは `UID SEARCH UID <前回のUIDNEXT>:*` だけで済み、
// SEARCH SINCE を使わない。
//
// 背景: go-imap は SINCE の日付を quoted string で送る (RFC 3501 では合法) が、
// Apache James はこれを "BAD UID failed. Illegal arguments." で蹴る。
// SINCE に頼ると James では受信箱の取込みが一切動かないため、初回は
// INTERNALDATE で、以降は UID の続きで絞る。
//
// worker ごとに 1 つ持ち、DialIMAP の後に WithCursor で client に渡す。
// UIDVALIDITY が変わったら捨てて初回扱いに戻る。
type UIDCursor struct {
	mu   sync.Mutex
	next map[string]uidCursorEntry
}

type uidCursorEntry struct {
	validity uint32
	next     imap.UID
}

// NewUIDCursor は空のカーソルを返す。
func NewUIDCursor() *UIDCursor {
	return &UIDCursor{next: map[string]uidCursorEntry{}}
}

func (c *UIDCursor) get(key string) (uidCursorEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.next[key]
	return e, ok
}

func (c *UIDCursor) set(key string, e uidCursorEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next[key] = e
}

// WithCursor は取込み位置の記憶先を設定する (メソッドチェーン用に自身を返す)。
func (ic *IMAPClient) WithCursor(cur *UIDCursor) *IMAPClient {
	if ic != nil {
		ic.cursor = cur
	}
	return ic
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

	return &IMAPClient{c: c, cfg: cfg, username: cfg.Username}, nil
}

// reconnect は接続を張り直して LOGIN し、直前に SELECT していた mailbox を
// read-only で SELECT し直す。パース失敗で go-imap が接続を閉じたあとの復帰用。
func (ic *IMAPClient) reconnect() error {
	if ic.c != nil {
		_ = ic.c.Close()
	}
	nc, err := DialIMAP(ic.cfg)
	if err != nil {
		return err
	}
	ic.c = nc.c
	if ic.selected != "" {
		if _, err := ic.c.Select(ic.selected, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return fmt.Errorf("imap: reselect %q: %w", ic.selected, err)
		}
	}
	return nil
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
	return ic.fetchSince(mailbox, since, false)
}

// FetchSinceFull は FetchSince に加えて本文 (text/plain、無ければ HTML の
// タグ除去) と添付ファイル名も取得する。BODY[] を丸ごと取るぶん envelope-only
// より重いので、MailboxMessage 取込み (Phase 26) 専用。
func (ic *IMAPClient) FetchSinceFull(mailbox string, since time.Time) ([]ParsedMessage, error) {
	return ic.fetchSince(mailbox, since, true)
}

func (ic *IMAPClient) fetchSince(mailbox string, since time.Time, withBody bool) ([]ParsedMessage, error) {
	if ic == nil || ic.c == nil {
		return nil, errors.New("imap: client not connected")
	}

	sel, err := ic.c.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap: select %q: %w", mailbox, err)
	}
	ic.selected = mailbox

	uids, err := ic.selectUIDs(mailbox, sel, since)
	if err != nil {
		return nil, err
	}
	// 取込みが成功した時だけカーソルを進める (失敗したら次回同じ範囲をやり直す)。
	advance := func() {
		if ic.cursor != nil {
			ic.cursor.set(ic.cursorKey(mailbox), uidCursorEntry{validity: sel.UIDValidity, next: sel.UIDNext})
		}
	}
	if len(uids) == 0 {
		advance()
		return nil, nil
	}

	fetchOpts := &imap.FetchOptions{
		Envelope: true,
		UID:      true,
	}
	var bodySection *imap.FetchItemBodySection
	if withBody {
		bodySection = &imap.FetchItemBodySection{} // BODY[] = ヘッダ含む全文
		fetchOpts.BodySection = []*imap.FetchItemBodySection{bodySection}
	}

	// 一度の FETCH に載せる UID を区切る。数千通を 1 コマンドで取ると
	// James が slow reader として backpressure をかけ、途中で終わることがある
	// (2026-10-30 のバックフィルで 10/4 以降が丸ごと欠けた)。
	msgs := make([]ParsedMessage, 0, len(uids))
	for start := 0; start < len(uids); start += fetchChunkSize {
		end := start + fetchChunkSize
		if end > len(uids) {
			end = len(uids)
		}
		chunk, err := ic.fetchResilient(uids[start:end], fetchOpts, bodySection, withBody)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, chunk...)
	}
	advance()
	return msgs, nil
}

// fetchChunkSize は 1 回の FETCH に載せる UID 数の上限。
// James は複数通をまとめて FETCH すると応答が途中で崩れることがある
// (1 通ずつなら通る) ので小さめにし、崩れたら fetchResilient が半分に割って
// 取り直す。
const fetchChunkSize = 5

// fetchResilient は fetchChunk が失敗したら再接続して UID 列を半分に割り、
// 1 通まで割っても取れないものは警告を出して飛ばす (取込み全体を止めない)。
func (ic *IMAPClient) fetchResilient(uids []imap.UID, fetchOpts *imap.FetchOptions, bodySection *imap.FetchItemBodySection, withBody bool) ([]ParsedMessage, error) {
	msgs, err := ic.fetchChunk(uids, fetchOpts, bodySection, withBody)
	if err == nil {
		return msgs, nil
	}
	// go-imap はパース失敗で接続を閉じるので張り直す。再接続自体が駄目なら諦める。
	if rerr := ic.reconnect(); rerr != nil {
		return nil, fmt.Errorf("%w (reconnect failed: %v)", err, rerr)
	}
	if len(uids) == 1 {
		log.Warn().Err(err).Str("user", ic.username).Str("mailbox", ic.selected).Uint32("uid", uint32(uids[0])).
			Msg("imap: message skipped — fetch keeps failing")
		return nil, nil
	}
	mid := len(uids) / 2
	left, err := ic.fetchResilient(uids[:mid], fetchOpts, bodySection, withBody)
	if err != nil {
		return nil, err
	}
	right, err := ic.fetchResilient(uids[mid:], fetchOpts, bodySection, withBody)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

// fetchChunk は UID 列を 1 回の FETCH で取り、ParsedMessage にする。
// FETCH が途中で NO/BAD や切断で終わった場合はエラーにする (黙って部分結果を
// 返さない — カーソルを進めてよいのは全件取れたときだけ)。
func (ic *IMAPClient) fetchChunk(uids []imap.UID, fetchOpts *imap.FetchOptions, bodySection *imap.FetchItemBodySection, withBody bool) ([]ParsedMessage, error) {
	uidSet := imap.UIDSet{}
	for _, u := range uids {
		uidSet.AddNum(u)
	}
	fetchCmd := ic.c.Fetch(uidSet, fetchOpts)

	msgs := make([]ParsedMessage, 0, len(uids))
	for {
		m := fetchCmd.Next()
		if m == nil {
			break
		}
		buf, err := m.Collect()
		if err != nil {
			_ = fetchCmd.Close()
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
		for _, id := range buf.Envelope.InReplyTo {
			if n := normalizeMessageID(id); n != "" {
				pm.InReplyTo = append(pm.InReplyTo, n)
			}
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
		if withBody {
			if raw := buf.FindBodySection(bodySection); len(raw) > 0 {
				body, atts := parseBodyAndAttachments(raw)
				pm.Body = body
				pm.AttachmentNames = atts
				// Phase 27c: References + DSN は生ヘッダからのみ取れる
				pm.References = parseReferences(raw)
				pm.DSN = ParseDSN(raw)
			}
		}
		msgs = append(msgs, pm)
	}
	if err := fetchCmd.Close(); err != nil {
		return nil, fmt.Errorf("imap: fetch: %w", err)
	}
	return msgs, nil
}

func (ic *IMAPClient) cursorKey(mailbox string) string {
	return ic.username + "\x00" + mailbox
}

// selectUIDs は取込み対象の UID 一覧を決める。SEARCH SINCE は使わない
// (UIDCursor のコメント参照)。
//   - since ゼロ値            → 全件 (初回バックフィル)
//   - カーソルあり (同じ UIDVALIDITY) → 前回の UIDNEXT 以降だけ
//   - それ以外 (起動直後など)   → 全 UID の INTERNALDATE を見て since 以降に絞る
func (ic *IMAPClient) selectUIDs(mailbox string, sel *imap.SelectData, since time.Time) ([]imap.UID, error) {
	if sel.NumMessages == 0 {
		return nil, nil
	}
	if since.IsZero() {
		return ic.searchUIDs(&imap.SearchCriteria{})
	}
	if ic.cursor != nil {
		if e, ok := ic.cursor.get(ic.cursorKey(mailbox)); ok && e.validity == sel.UIDValidity {
			if sel.UIDNext <= e.next {
				return nil, nil
			}
			set := imap.UIDSet{}
			set.AddRange(e.next, 0) // "<next>:*"
			uids, err := ic.searchUIDs(&imap.SearchCriteria{UID: []imap.UIDSet{set}})
			if err != nil {
				return nil, err
			}
			// "n:*" は n より大きいメッセージが無いと最後の 1 通を返すので念のため絞る
			out := uids[:0]
			for _, u := range uids {
				if u >= e.next {
					out = append(out, u)
				}
			}
			return out, nil
		}
	}
	return ic.uidsSince(since)
}

func (ic *IMAPClient) searchUIDs(criteria *imap.SearchCriteria) ([]imap.UID, error) {
	data, err := ic.c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap: search: %w", err)
	}
	return data.AllUIDs(), nil
}

// uidsSince は全 UID の INTERNALDATE を取り、since の日 (00:00) 以降のものを返す。
// 旧実装の SEARCH SINCE と同じ「日付単位」の絞り込みに揃える。
func (ic *IMAPClient) uidsSince(since time.Time) ([]imap.UID, error) {
	all, err := ic.searchUIDs(&imap.SearchCriteria{})
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}
	set := imap.UIDSet{}
	for _, u := range all {
		set.AddNum(u)
	}
	y, m, d := since.Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, since.Location())

	cmd := ic.c.Fetch(set, &imap.FetchOptions{UID: true, InternalDate: true})
	defer cmd.Close()
	var out []imap.UID
	for {
		msg := cmd.Next()
		if msg == nil {
			break
		}
		buf, err := msg.Collect()
		if err != nil {
			return nil, fmt.Errorf("imap: collect internaldate: %w", err)
		}
		if buf.InternalDate.IsZero() || !buf.InternalDate.Before(dayStart) {
			out = append(out, buf.UID)
		}
	}
	return out, nil
}

// normalizeMessageID は `<abc@host>` 形式の angle brackets を剥がす。
// phox 側 DB の `message_id` カラムも bracket 無し形式で統一する。
func normalizeMessageID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	return s
}
