package mail

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

// IMAPWorkerConfig — 接続 + polling + 対象 mailbox の設定。
//
//   - Host/Port/TLSMode/Username/Password は mailu / gmail / dovecot 等の
//     IMAP サーバーを指す。
//   - SentMailbox は Phox ユーザーが「外部クライアントから送った」メールを拾う
//     ための Sent フォルダ (例: "Sent" / "送信済み" / "INBOX.Sent")。
//   - InboxMailbox は受信メールフォルダ (例: "INBOX")。
//   - PollInterval は polling 間隔 ("30s" / "1m" / "5m" 等、time.ParseDuration 形式)。
//     未指定時はデフォ 60 秒。
//   - IngestUserID は取込んだ Activity の user_id カラムに入れる値。基本は
//     IMAP worker 起動時の "system" シード行 (000003 mig で挿入済) を使う。
type IMAPWorkerConfig struct {
	Host                  string
	Port                  int
	TLSMode               IMAPTLSMode
	TLSInsecureSkipVerify bool
	Username              string
	Password              string
	SentMailbox           string
	InboxMailbox          string
	PollInterval          string
	IngestUserID          string
}

// IMAPWorker は mailu の Sent + INBOX を polling し、To/From が既存の
// Customer.mail / Contact.mail に一致するメッセージを Activity として取込む。
//
// 本実装 (Phase 14b) は emersion/go-imap v2 を使って polling loop を走らせ、
// 結果を `queries.CreateActivity` で DB に書く。dedup は `message_id` 列の
// UNIQUE INDEX に任せる (二重取込みは DB が拒否 → ignore)。
type IMAPWorker struct {
	cfg     IMAPWorkerConfig
	queries *db.Queries
}

// NewIMAPWorker は cfg と Phox の sqlc queries を受けて worker を返す。
// cfg.Host が空なら `Enabled()=false` になり、main.go は起動をスキップする。
func NewIMAPWorker(cfg IMAPWorkerConfig, queries *db.Queries) *IMAPWorker {
	if cfg.SentMailbox == "" {
		cfg.SentMailbox = "Sent"
	}
	if cfg.InboxMailbox == "" {
		cfg.InboxMailbox = "INBOX"
	}
	if cfg.IngestUserID == "" {
		cfg.IngestUserID = "system"
	}
	return &IMAPWorker{cfg: cfg, queries: queries}
}

// Enabled は IMAP_HOST が設定されているかを返す。
func (w *IMAPWorker) Enabled() bool {
	return w != nil && w.cfg.Host != ""
}

// Run は polling loop のエントリポイント。ctx がキャンセルされるまで
// 定期的に fetchAndIngest を呼ぶ。
func (w *IMAPWorker) Run(ctx context.Context) error {
	if !w.Enabled() {
		return nil
	}

	interval, err := time.ParseDuration(w.cfg.PollInterval)
	if err != nil || interval <= 0 {
		interval = 60 * time.Second
	}

	log.Info().
		Str("host", w.cfg.Host).
		Int("port", w.cfg.Port).
		Str("sent", w.cfg.SentMailbox).
		Str("inbox", w.cfg.InboxMailbox).
		Dur("interval", interval).
		Msg("IMAP worker: starting polling loop")

	// 起動直後に 1 回走らせ、以降は interval ごと。
	if err := w.tick(ctx); err != nil {
		log.Warn().Err(err).Msg("IMAP worker: initial tick failed")
	}

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := w.tick(ctx); err != nil {
				log.Warn().Err(err).Msg("IMAP worker: tick failed")
			}
		}
	}
}

// tick は 1 回の polling サイクルを実行する。
// 接続 → Sent fetch → INBOX fetch → 切断。
func (w *IMAPWorker) tick(ctx context.Context) error {
	client, err := DialIMAP(IMAPConnectConfig{
		Host:                  w.cfg.Host,
		Port:                  w.cfg.Port,
		Username:              w.cfg.Username,
		Password:              w.cfg.Password,
		TLSMode:               w.cfg.TLSMode,
		TLSInsecureSkipVerify: w.cfg.TLSInsecureSkipVerify,
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = client.Close() }()

	since := w.resolveSince(ctx)

	// Sent: 外部クライアントから送信したメールを email_sent として取込み
	if msgs, err := client.FetchSince(w.cfg.SentMailbox, since); err != nil {
		log.Warn().Err(err).Str("mailbox", w.cfg.SentMailbox).Msg("IMAP worker: sent fetch")
	} else {
		w.ingestBatch(ctx, msgs, "email_sent")
	}

	// INBOX: 顧客から受信したメール返信を email_received として取込み
	if msgs, err := client.FetchSince(w.cfg.InboxMailbox, since); err != nil {
		log.Warn().Err(err).Str("mailbox", w.cfg.InboxMailbox).Msg("IMAP worker: inbox fetch")
	} else {
		w.ingestBatch(ctx, msgs, "email_received")
	}

	return nil
}

// resolveSince は今回の tick で fetch する範囲の開始日時を決定する。
// 既に DB に取込まれている Activity の最新 occurred_at より新しいものを
// 取りに行く (ただし SEARCH SINCE は日単位なので実際は当日 00:00 まで戻る)。
// Activity が 0 件なら 24 時間前を既定。
func (w *IMAPWorker) resolveSince(ctx context.Context) time.Time {
	// 簡略化: w.queries に "MAX(occurred_at) WHERE type IN ('email_sent','email_received')"
	// を直接返すクエリは無いので、24 時間前 default でまず投入 (dedup は message_id に任せる)。
	// 本来は新しい sqlc query を足す価値あり — Phase 14 の拡張候補。
	_ = ctx
	return time.Now().Add(-24 * time.Hour)
}

// ingestBatch はレガシー単一メールボックス worker 用の薄いラッパ。
// mailbox_id は付けない (env 単一アカウント = メールボックス管理対象外)。
func (w *IMAPWorker) ingestBatch(ctx context.Context, msgs []ParsedMessage, activityType string) {
	ingestMessages(ctx, w.queries, msgs, activityType, w.cfg.IngestUserID, pgtype.UUID{Valid: false})
}

// ingestMessages は fetch 結果を 1 行ずつ DB に insert する共通ロジック。
// legacy IMAPWorker と DB 駆動の MailboxIMAPWorker の両方が使う。
// - To / From を正規化して `FindCustomerByEmail` で Customer + Contact を解決
//   - 解決できないメールは skip (CRM 管理外)
//   - `CreateActivity` で insert。`message_id` UNIQUE INDEX が dedup を保証
//   - mailboxID を渡すと Activity.mailbox_id に記録する (どのメールボックスで
//     送受信したか)。
func ingestMessages(ctx context.Context, queries *db.Queries, msgs []ParsedMessage, activityType, ingestUserID string, mailboxID pgtype.UUID) {
	for _, m := range msgs {
		if m.MessageID == "" {
			// RFC5322 準拠ではないメールは skip
			continue
		}

		// match 先のアドレスを決定: email_sent なら To (送信先) 側を、
		// email_received なら From (送信元) 側を Customer とみなす。
		var matchAddrs []string
		switch activityType {
		case "email_sent":
			matchAddrs = m.To
		case "email_received":
			if m.From != "" {
				matchAddrs = []string{m.From}
			}
		}

		customer, contact, ok := resolveCustomer(ctx, queries, matchAddrs)

		// Phase 26: 管理対象メールボックス経由なら、顧客に紐付かなくても
		// 全メッセージを MailboxMessage に保存する (Activity dedup より先に —
		// 過去に Activity 化済みのメールもここには入れる)。
		if mailboxID.Valid {
			storeMailboxMessage(ctx, queries, m, activityType, mailboxID, customer, ok)
		}

		// 既に取込済みなら dedup (UNIQUE INDEX でも止まるが事前チェックでログを減らす)
		if _, err := queries.GetActivityByMessageID(ctx, pgtype.Text{String: m.MessageID, Valid: true}); err == nil {
			continue
		} else if !errors.Is(err, pgx.ErrNoRows) {
			log.Warn().Err(err).Str("message_id", m.MessageID).Msg("IMAP worker: dedup lookup failed")
			continue
		}

		if !ok {
			log.Debug().
				Strs("addrs", matchAddrs).
				Str("message_id", m.MessageID).
				Msg("IMAP worker: no matching customer — activity skipped (raw message kept)")
			continue
		}

		// CreateActivity
		params := db.CreateActivityParams{
			ID:         uuid.New(),
			CustomerID: customer,
			ContactID:  pgtype.UUID{Valid: false},
			Type:       activityType,
			UserID:     ingestUserID,
			StatusID:   pgtype.UUID{Valid: false},
			MailFrom:   pgtype.Text{String: m.From, Valid: m.From != ""},
			Subject:    pgtype.Text{String: m.Subject, Valid: m.Subject != ""},
			Body:       pgtype.Text{String: m.Body, Valid: m.Body != ""},
			MessageID:  pgtype.Text{String: m.MessageID, Valid: true},
			OccurredAt: effectiveOccurredAt(m),
			MailboxID:  mailboxID,
		}
		if contact != uuid.Nil {
			params.ContactID = pgtype.UUID{Bytes: contact, Valid: true}
		}
		if len(m.To) > 0 {
			params.MailTo = pgtype.Text{String: strings.Join(m.To, ", "), Valid: true}
		}
		if len(m.Cc) > 0 {
			params.MailCc = pgtype.Text{String: strings.Join(m.Cc, ", "), Valid: true}
		}

		if _, err := queries.CreateActivity(ctx, params); err != nil {
			// UNIQUE 違反は正常 (dedup)
			if isUniqueViolation(err) {
				continue
			}
			log.Warn().Err(err).Str("message_id", m.MessageID).Msg("IMAP worker: insert activity")
			continue
		}
		log.Info().
			Str("type", activityType).
			Str("message_id", m.MessageID).
			Str("customer_id", customer.String()).
			Msg("IMAP worker: ingested activity")
	}
}

// storeMailboxMessage は 1 メッセージを MailboxMessage に保存する (Phase 26)。
// 顧客解決の成否に関わらず全メッセージが対象。dedup は
// (mailbox_id, message_id) UNIQUE INDEX に任せ、違反は正常系として無視。
func storeMailboxMessage(ctx context.Context, queries *db.Queries, m ParsedMessage, activityType string, mailboxID pgtype.UUID, customer uuid.UUID, resolved bool) {
	folder := "INBOX"
	if activityType == "email_sent" {
		folder = "Sent"
	}
	params := db.CreateMailboxMessageParams{
		ID:              uuid.New(),
		MailboxID:       mailboxID.Bytes,
		Folder:          folder,
		MessageID:       m.MessageID,
		FromAddr:        m.From,
		ToAddrs:         strings.Join(m.To, ", "),
		CcAddrs:         strings.Join(m.Cc, ", "),
		Subject:         m.Subject,
		BodyText:        m.Body,
		AttachmentNames: strings.Join(m.AttachmentNames, ", "),
		OccurredAt:      effectiveOccurredAt(m),
	}
	if resolved && customer != uuid.Nil {
		params.CustomerID = pgtype.UUID{Bytes: customer, Valid: true}
	}
	if _, err := queries.CreateMailboxMessage(ctx, params); err != nil {
		if isUniqueViolation(err) {
			return // 取込済み
		}
		log.Warn().Err(err).Str("message_id", m.MessageID).Msg("IMAP worker: insert mailbox message")
	}
}

// resolveCustomer は match 候補アドレス列から、最初にヒットした
// Customer (+ 任意の Contact) を返す。
// FindCustomerByEmail は Customer.mail と Contact.mail の両方を UNION で引く
// (Phase 9 で追加済) ので、どちらにヒットしても OK。
func resolveCustomer(ctx context.Context, queries *db.Queries, addrs []string) (uuid.UUID, uuid.UUID, bool) {
	for _, a := range addrs {
		addr := normalizeEmail(a)
		if addr == "" {
			continue
		}
		row, err := queries.FindCustomerByEmail(ctx, addr)
		if err != nil {
			continue
		}
		var contactID uuid.UUID
		if row.ContactID.Valid {
			contactID = row.ContactID.Bytes
		}
		return row.CustomerID, contactID, true
	}
	return uuid.Nil, uuid.Nil, false
}

// normalizeEmail は `"名前" <foo@bar>` → `foo@bar` に縮める。
// go-imap の Address.Addr() は既に純粋アドレスを返すが、念のため defensive。
func normalizeEmail(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "<"); i >= 0 {
		if j := strings.Index(s, ">"); j > i {
			s = s[i+1 : j]
		}
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// effectiveOccurredAt は envelope.Date が取れていればそれを、取れなければ
// now() を返す。
func effectiveOccurredAt(m ParsedMessage) time.Time {
	if !m.Date.IsZero() {
		return m.Date
	}
	return time.Now()
}

// isUniqueViolation は pgx の unique 違反エラーを検出する。
// Phase 20 の internal/service/book/default_status.go と同じ実装を
// 別 package で再利用するために複製 (循環依存を避けるため)。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "23505") ||
		strings.Contains(s, "duplicate key") ||
		strings.Contains(s, "unique constraint")
}
