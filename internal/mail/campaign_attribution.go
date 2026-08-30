package mail

import (
	"context"
	"regexp"
	"strings"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
)

// campaignReplyLookbackDays: from アドレスのフォールバック照合で遡る日数。
// 日本の商習慣では「返信」でなく新規メールで返ってくることが多く、ヘッダの
// In-Reply-To/References だけでは取りこぼすため。
const campaignReplyLookbackDays = 60

// attributeCampaignReply は受信メールをキャンペーン受信者に紐付けて
// replied_at + reply イベントを記録する (Phase 27c)。冪等 (COALESCE)。
//
//  1. In-Reply-To / References の Message-ID を cmp-* 規約で引き当てる
//  2. ヘッダで取れなければ from アドレスで直近 60 日の sent 受信者に当てる
func attributeCampaignReply(ctx context.Context, queries *db.Queries, m ParsedMessage) {
	rec, ok := findRecipientByHeaders(ctx, queries, m)
	if !ok && m.From != "" {
		rows, err := queries.FindSentRecipientByFromAddr(ctx, db.FindSentRecipientByFromAddrParams{
			Email:  strings.ToLower(m.From),
			SentAt: pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, -campaignReplyLookbackDays), Valid: true},
		})
		if err == nil && len(rows) > 0 {
			rec, ok = rows[0], true // 直近に送ったキャンペーンへ帰属
		}
	}
	if !ok {
		return
	}
	alreadyReplied := rec.RepliedAt.Valid
	if err := queries.MarkRecipientReplied(ctx, rec.ID); err != nil {
		log.Warn().Err(err).Str("recipient", rec.ID.String()).Msg("campaign: mark replied failed")
		return
	}
	if !alreadyReplied {
		if err := queries.CreateCampaignEvent(ctx, db.CreateCampaignEventParams{
			ID:          uuid.New(),
			RecipientID: rec.ID,
			Kind:        "reply",
			Url:         "",
			UserAgent:   "",
		}); err != nil {
			log.Warn().Err(err).Msg("campaign: create reply event failed")
		}
		log.Info().
			Str("recipient", rec.ID.String()).
			Str("from", m.From).
			Msg("campaign: reply attributed")
	}
}

// campaignMessageIDRe は cmp-{recipient_uuid}[-s{step}]@... 形式の Message-ID。
// Phase 27e 以降フォローアップは -sN サフィックス付きなので、UUID を直接抜いて
// 受信者を引く (message_id カラムの完全一致に依存しない)。
var campaignMessageIDRe = regexp.MustCompile(`^cmp-([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:-s\d+)?@`)

// recipientIDFromMessageID は cmp-* Message-ID から受信者 UUID を取り出す。
func recipientIDFromMessageID(id string) (uuid.UUID, bool) {
	m := campaignMessageIDRe.FindStringSubmatch(id)
	if m == nil {
		return uuid.Nil, false
	}
	rid, err := uuid.Parse(m[1])
	return rid, err == nil
}

// findRecipientByHeaders は In-Reply-To + References から cmp-* Message-ID を
// 探して受信者を引き当てる。
func findRecipientByHeaders(ctx context.Context, queries *db.Queries, m ParsedMessage) (db.CampaignRecipient, bool) {
	candidates := append(append([]string{}, m.InReplyTo...), m.References...)
	for _, id := range candidates {
		rid, ok := recipientIDFromMessageID(id)
		if !ok {
			continue // キャンペーン規約外の ID は DB を引かない
		}
		rec, err := queries.GetCampaignRecipient(ctx, rid)
		if err == nil {
			return rec, true
		}
	}
	return db.CampaignRecipient{}, false
}

// handleCampaignBounce は DSN をキャンペーン受信者に紐付けてバウンス処理する。
// ハードバウンス (5.x.x) は bounced_at + Suppression(hard_bounce) で以後の
// 全キャンペーンから恒久除外。ソフト (4.x.x) はイベント記録のみ。
// 戻り値は「処理した (= DSN として消費した)」かどうか。
func handleCampaignBounce(ctx context.Context, queries *db.Queries, m ParsedMessage) bool {
	if m.DSN == nil {
		return false
	}
	var rec db.CampaignRecipient
	found := false
	if rid, ok := recipientIDFromMessageID(m.DSN.OriginalMessageID); ok {
		if r, err := queries.GetCampaignRecipient(ctx, rid); err == nil {
			rec, found = r, true
		}
	}
	if !found && m.DSN.FinalRecipient != "" {
		rows, err := queries.FindSentRecipientByFromAddr(ctx, db.FindSentRecipientByFromAddrParams{
			Email:  m.DSN.FinalRecipient,
			SentAt: pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, -campaignReplyLookbackDays), Valid: true},
		})
		if err == nil && len(rows) > 0 {
			rec, found = rows[0], true
		}
	}
	if !found {
		// キャンペーン外のメールへの DSN (通常メールのバウンス等)。
		// DSN であること自体は真なので true を返して Activity 化は抑止する。
		return true
	}

	alreadyBounced := rec.BouncedAt.Valid
	kind := "soft"
	if m.DSN.IsHard() {
		kind = "hard"
		if err := queries.MarkRecipientBounced(ctx, rec.ID); err != nil {
			log.Warn().Err(err).Str("recipient", rec.ID.String()).Msg("campaign: mark bounced failed")
		}
		c, cerr := queries.GetCampaign(ctx, rec.CampaignID)
		if cerr == nil {
			if serr := queries.CreateSuppression(ctx, db.CreateSuppressionParams{
				ID:         uuid.New(),
				CompanyID:  c.CompanyID,
				Lower:      rec.Email,
				Reason:     "hard_bounce",
				CampaignID: pgtype.UUID{Bytes: c.ID, Valid: true},
				Note:       "DSN " + m.DSN.Status,
			}); serr != nil {
				log.Warn().Err(serr).Msg("campaign: create hard_bounce suppression failed")
			}
		}
	}
	if !alreadyBounced {
		if err := queries.CreateCampaignEvent(ctx, db.CreateCampaignEventParams{
			ID:          uuid.New(),
			RecipientID: rec.ID,
			Kind:        "bounce",
			Url:         "", // url 列は流用しない — 詳細は note/status をログへ
			UserAgent:   kind + " " + m.DSN.Status,
		}); err != nil {
			log.Warn().Err(err).Msg("campaign: create bounce event failed")
		}
	}
	log.Info().
		Str("recipient", rec.ID.String()).
		Str("status", m.DSN.Status).
		Str("kind", kind).
		Msg("campaign: bounce attributed")
	return true
}
