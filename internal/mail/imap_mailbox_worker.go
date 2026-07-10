package mail

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
)

// MailboxIMAPWorker は DB の active な Mailbox を全て polling する worker
// (Phase 25/C)。共有 mailu 接続 (MAILU_IMAP_*) に対し、各メールボックスの
// 資格情報 (復号) で接続して Sent + INBOX を取込み、Activity.mailbox_id を
// 記録する。取込みロジック自体は legacy IMAPWorker と共有 (ingestMessages)。
//
// legacy の env 単一 IMAPWorker とは独立に動く。両方設定されている環境で
// 同じアカウントを二重 polling しないよう、運用側で片方に寄せること
// (dedup は message_id UNIQUE INDEX が保証するので二重取込みにはならない)。
type MailboxIMAPWorker struct {
	conn         IMAPConnBase
	sentMailbox  string
	inboxMailbox string
	pollInterval string
	ingestUserID string
	queries      *db.Queries
	cipher       *crypto.Cipher
}

// IMAPConnBase は全メールボックス共通の接続パラメータ (資格情報を除く)。
type IMAPConnBase struct {
	Host                  string
	Port                  int
	TLSMode               IMAPTLSMode
	TLSInsecureSkipVerify bool
}

// NewMailboxIMAPWorker は conn.Host が空、または cipher が nil なら
// nil を返す (機能無効)。
func NewMailboxIMAPWorker(conn IMAPConnBase, sentMailbox, inboxMailbox, pollInterval, ingestUserID string, queries *db.Queries, cipher *crypto.Cipher) *MailboxIMAPWorker {
	if conn.Host == "" || cipher == nil {
		return nil
	}
	if sentMailbox == "" {
		sentMailbox = "Sent"
	}
	if inboxMailbox == "" {
		inboxMailbox = "INBOX"
	}
	if ingestUserID == "" {
		ingestUserID = "system"
	}
	return &MailboxIMAPWorker{
		conn:         conn,
		sentMailbox:  sentMailbox,
		inboxMailbox: inboxMailbox,
		pollInterval: pollInterval,
		ingestUserID: ingestUserID,
		queries:      queries,
		cipher:       cipher,
	}
}

// Run は polling loop。ctx キャンセルまで interval ごとに全メールボックスを回す。
func (w *MailboxIMAPWorker) Run(ctx context.Context) error {
	if w == nil {
		return nil
	}
	interval, err := time.ParseDuration(w.pollInterval)
	if err != nil || interval <= 0 {
		interval = 60 * time.Second
	}
	log.Info().
		Str("host", w.conn.Host).
		Int("port", w.conn.Port).
		Dur("interval", interval).
		Msg("Mailbox IMAP worker: starting polling loop")

	if err := w.tick(ctx); err != nil {
		log.Warn().Err(err).Msg("Mailbox IMAP worker: initial tick failed")
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := w.tick(ctx); err != nil {
				log.Warn().Err(err).Msg("Mailbox IMAP worker: tick failed")
			}
		}
	}
}

// tick は DB の active な Mailbox を全て取り、それぞれ polling する。
// synced_at が NULL のメールボックスは初回全履歴バックフィル (since ゼロ値 =
// IMAP SEARCH に SINCE を付けない = 全件)。成功したら synced_at を刻み、
// 以降は直近 24h の incremental (dedup は UNIQUE INDEX が保証)。
func (w *MailboxIMAPWorker) tick(ctx context.Context) error {
	mailboxes, err := w.queries.ListAllActiveMailboxes(ctx)
	if err != nil {
		return fmt.Errorf("list active mailboxes: %w", err)
	}
	incremental := time.Now().Add(-24 * time.Hour)
	for _, mb := range mailboxes {
		since := incremental
		backfill := !mb.SyncedAt.Valid
		if backfill {
			since = time.Time{} // 全履歴
			log.Info().Str("mailbox", mb.Address).Msg("Mailbox IMAP worker: first sync — full backfill")
		}
		if ok := w.pollOne(ctx, mb, since); ok && backfill {
			if err := w.queries.SetMailboxSyncedAt(ctx, mb.ID); err != nil {
				log.Warn().Err(err).Str("mailbox", mb.Address).Msg("Mailbox IMAP worker: set synced_at")
			}
		}
	}
	return nil
}

// pollOne は 1 メールボックスに接続して Sent/INBOX を取込む。
// 1 つのメールボックスの失敗が他をブロックしないよう、エラーは log のみ。
// 戻り値は両フォルダの fetch が成功したかどうか (バックフィル完了判定用)。
func (w *MailboxIMAPWorker) pollOne(ctx context.Context, mb db.Mailbox, since time.Time) bool {
	password, err := w.cipher.DecryptString(mb.PasswordEnc)
	if err != nil {
		log.Warn().Err(err).Str("mailbox", mb.Address).Msg("Mailbox IMAP worker: decrypt password")
		return false
	}
	client, err := DialIMAP(IMAPConnectConfig{
		Host:                  w.conn.Host,
		Port:                  w.conn.Port,
		Username:              mb.SmtpUsername,
		Password:              password,
		TLSMode:               w.conn.TLSMode,
		TLSInsecureSkipVerify: w.conn.TLSInsecureSkipVerify,
	})
	if err != nil {
		log.Warn().Err(err).Str("mailbox", mb.Address).Msg("Mailbox IMAP worker: dial")
		return false
	}
	defer func() { _ = client.Close() }()

	mailboxID := pgtype.UUID{Bytes: mb.ID, Valid: true}
	allOK := true

	if msgs, ferr := client.FetchSinceFull(w.sentMailbox, since); ferr != nil {
		log.Warn().Err(ferr).Str("mailbox", mb.Address).Str("folder", w.sentMailbox).Msg("Mailbox IMAP worker: sent fetch")
		allOK = false
	} else {
		ingestMessages(ctx, w.queries, msgs, "email_sent", w.ingestUserID, mailboxID)
	}
	if msgs, ferr := client.FetchSinceFull(w.inboxMailbox, since); ferr != nil {
		log.Warn().Err(ferr).Str("mailbox", mb.Address).Str("folder", w.inboxMailbox).Msg("Mailbox IMAP worker: inbox fetch")
		allOK = false
	} else {
		ingestMessages(ctx, w.queries, msgs, "email_received", w.ingestUserID, mailboxID)
	}
	return allOK
}
