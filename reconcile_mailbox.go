package main

import (
	"context"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// runReconcileMailbox は全 Customer について、mail に一致する未紐付け
// MailboxMessage を Activity 化 + customer_id 紐付けする一括バックフィル。
// `phox-customer reconcile-mailbox` で実行。冪等 (message_id 重複はスキップ)。
//
// メンテナンス CLI。メール取込み → 顧客登録 の順で作業した過去分や、
// 複数メールボックス間でタイミングがずれた分をまとめて履歴に載せ直す。
func runReconcileMailbox(cfg util.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DBSource)
	if err != nil {
		log.Fatal().Err(err).Msg("reconcile: failed to connect to db")
	}
	defer pool.Close()
	queries := db.New(pool)

	customers, err := queries.ListAllCustomers(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("reconcile: failed to list customers")
	}

	var scanned, withMail int
	var linked int64
	for _, c := range customers {
		scanned++
		if c.Mail == "" {
			continue
		}
		withMail++
		n, err := queries.BackfillActivitiesForCustomerEmail(ctx, db.BackfillActivitiesForCustomerEmailParams{
			Email:      c.Mail,
			CustomerID: pgtype.UUID{Bytes: c.ID, Valid: true},
		})
		if err != nil {
			log.Warn().Err(err).Str("mail", c.Mail).Str("customer_id", c.ID.String()).
				Msg("reconcile: backfill failed for customer")
			continue
		}
		if n > 0 {
			linked += n
			log.Info().Int64("linked", n).Str("mail", c.Mail).Str("name", c.Name).
				Msg("reconcile: linked mailbox messages")
		}
	}

	log.Info().
		Int("customers_scanned", scanned).
		Int("customers_with_mail", withMail).
		Int64("messages_linked", linked).
		Msg("reconcile: done")
}
