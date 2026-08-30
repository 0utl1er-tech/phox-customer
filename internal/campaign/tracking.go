package campaign

import (
	"context"
	"fmt"
	"html"
	"net/http"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
)

// TrackingHandler は非認証の公開エンドポイント群。phox-api.0utl1er.tech は
// 既に公開されているので、トークンの HMAC 署名だけが防護壁 — 署名不一致は
// 一律 404 (存在を漏らさない)。
//
// Phase 27a は配信停止 (/u/{token}) のみ。開封 (/t/o) / クリック (/t/c) は 27b。
type TrackingHandler struct {
	queries   *db.Queries
	tokenizer *Tokenizer
}

func NewTrackingHandler(queries *db.Queries, tokenizer *Tokenizer) *TrackingHandler {
	if tokenizer == nil {
		return nil
	}
	return &TrackingHandler{queries: queries, tokenizer: tokenizer}
}

// Unsubscribe は GET (人間がリンクを踏む) と POST (RFC 8058 One-Click、
// メールクライアントが自動で叩く) の両方を受ける。どちらも同じ冪等処理。
// GET のプリフェッチ誤爆リスクは配信停止に限っては許容 (安全側に倒れる)。
func (h *TrackingHandler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	kind, recipientID, _, err := h.tokenizer.Parse(r.PathValue("token"))
	if err != nil || kind != KindUnsubscribe {
		http.NotFound(w, r)
		return
	}
	sender, ok := h.recordUnsubscribe(r.Context(), recipientID, r.UserAgent())
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPost {
		w.WriteHeader(http.StatusOK) // One-Click はボディ不要
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, unsubscribePage, html.EscapeString(sender))
}

// recordUnsubscribe は Suppression 登録 + 受信者への時刻打刻 + イベント記録を
// 冪等に行い、配信停止ページに表示する送信者名を返す。
func (h *TrackingHandler) recordUnsubscribe(ctx context.Context, recipientID uuid.UUID, userAgent string) (senderOrg string, ok bool) {
	rec, err := h.queries.GetCampaignRecipient(ctx, recipientID)
	if err != nil {
		return "", false
	}
	c, err := h.queries.GetCampaign(ctx, rec.CampaignID)
	if err != nil {
		return "", false
	}

	already := rec.UnsubscribedAt.Valid
	if _, err := h.queries.MarkRecipientUnsubscribed(ctx, recipientID); err != nil {
		log.Error().Err(err).Str("recipient", recipientID.String()).Msg("campaign: mark unsubscribed failed")
		return "", false
	}
	if err := h.queries.CreateSuppression(ctx, db.CreateSuppressionParams{
		ID:         uuid.New(),
		CompanyID:  c.CompanyID,
		Lower:      rec.Email,
		Reason:     "unsubscribe",
		CampaignID: pgtype.UUID{Bytes: c.ID, Valid: true},
		Note:       "",
	}); err != nil {
		log.Error().Err(err).Str("recipient", recipientID.String()).Msg("campaign: create suppression failed")
	}
	if !already {
		if err := h.queries.CreateCampaignEvent(ctx, db.CreateCampaignEventParams{
			ID:          uuid.New(),
			RecipientID: recipientID,
			Kind:        "unsubscribe",
			Url:         "",
			UserAgent:   truncate(userAgent, 255),
		}); err != nil {
			log.Error().Err(err).Msg("campaign: create unsubscribe event failed")
		}
		log.Info().Str("recipient", recipientID.String()).Msg("campaign: unsubscribed")
	}
	return c.SenderOrg, true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

const unsubscribePage = `<!doctype html>
<html lang="ja">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>配信停止</title>
<style>body{font-family:sans-serif;display:flex;justify-content:center;padding:4rem 1rem;background:#f5f5f5}main{background:#fff;border-radius:8px;padding:2rem;max-width:28rem;box-shadow:0 1px 4px rgba(0,0,0,.1)}h1{font-size:1.2rem}p{color:#444;line-height:1.7}</style>
</head>
<body><main>
<h1>配信停止が完了しました</h1>
<p>今後、%s からのご案内メールは送信されません。</p>
<p>お手数をおかけしました。</p>
</main></body></html>
`
