package campaign

import (
	"context"
	"fmt"
	"html"
	"net/http"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/notify"
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
	notifier  notify.Notifier // Phase 27h: 反響通知 (nil = 無効)
}

func NewTrackingHandler(queries *db.Queries, tokenizer *Tokenizer, notifier notify.Notifier) *TrackingHandler {
	if tokenizer == nil {
		return nil
	}
	return &TrackingHandler{queries: queries, tokenizer: tokenizer, notifier: notifier}
}

// notifyEvent は反響通知を送る (Phase 27h)。受信者×種別ごとの初回にだけ
// 呼ばれる前提 (already 判定は呼び出し側)。顧客名の解決に 1 クエリ増えるが
// 反響イベントは低頻度なので許容。
func (h *TrackingHandler) notifyEvent(ctx context.Context, rec db.CampaignRecipient, c db.Campaign, kind, url string) {
	if h.notifier == nil {
		return
	}
	var name, corp string
	if cust, err := h.queries.GetCustomer(ctx, rec.CustomerID); err == nil {
		name, corp = cust.Name, cust.Corporation
	}
	h.notifier.NotifyCampaignEvent(ctx, notify.CampaignEventInfo{
		Kind:         kind,
		CampaignID:   c.ID,
		CampaignName: c.Name,
		CustomerName: name,
		Corporation:  corp,
		Email:        rec.Email,
		URL:          url,
	})
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
		h.notifyEvent(ctx, rec, c, "unsubscribe", "")
	}
	return c.SenderOrg, true
}

// transparentGIF は 1×1 透過 GIF (43 byte)。
var transparentGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00,
	0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0x21, 0xf9, 0x04, 0x01, 0x00,
	0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
	0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
}

// Open は開封ピクセル (Phase 27b)。トークンが不正でも必ず GIF を返す
// (メールクライアントに壊れた画像を見せない)。メール画像プロキシの再取得で
// 複数ヒットするため、イベントは毎回記録し first_opened_at だけ冪等に打つ。
// 開封率が目安値なのは仕様 (画像ブロックで過小、プリフェッチで過大)。
func (h *TrackingHandler) Open(w http.ResponseWriter, r *http.Request) {
	kind, recipientID, _, err := h.tokenizer.Parse(r.PathValue("token"))
	if err == nil && kind == KindOpen {
		ctx := r.Context()
		if rec, gerr := h.queries.GetCampaignRecipient(ctx, recipientID); gerr == nil {
			// Phase 27h: first_opened_at がこのリクエストで初めて立つときだけ通知
			// (MarkRecipientOpened は COALESCE 冪等なので、更新前の rec で判定)。
			firstOpen := !rec.FirstOpenedAt.Valid
			_ = h.queries.MarkRecipientOpened(ctx, rec.ID)
			if err := h.queries.CreateCampaignEvent(ctx, db.CreateCampaignEventParams{
				ID:          uuid.New(),
				RecipientID: rec.ID,
				Kind:        "open",
				Url:         "",
				UserAgent:   truncate(r.UserAgent(), 255),
			}); err != nil {
				log.Error().Err(err).Msg("campaign: create open event failed")
			}
			if firstOpen {
				if c, cerr := h.queries.GetCampaign(ctx, rec.CampaignID); cerr == nil {
					h.notifyEvent(ctx, rec, c, "open", "")
				}
			}
		}
	}
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	_, _ = w.Write(transparentGIF)
}

// Click はクリックリダイレクト (Phase 27b)。リダイレクト先はトークンでなく
// CampaignLink (DB) から引くので open redirect にならない。
func (h *TrackingHandler) Click(w http.ResponseWriter, r *http.Request) {
	kind, recipientID, idx, err := h.tokenizer.Parse(r.PathValue("token"))
	if err != nil || kind != KindClick {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	rec, err := h.queries.GetCampaignRecipient(ctx, recipientID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	link, err := h.queries.GetCampaignLink(ctx, db.GetCampaignLinkParams{
		CampaignID: rec.CampaignID, Idx: int32(idx),
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Phase 27h: 初回クリックのみ通知 (first_clicked_at は COALESCE 冪等なので
	// 更新前の rec で判定)。
	firstClick := !rec.FirstClickedAt.Valid
	_ = h.queries.MarkRecipientClicked(ctx, rec.ID)
	if err := h.queries.CreateCampaignEvent(ctx, db.CreateCampaignEventParams{
		ID:          uuid.New(),
		RecipientID: rec.ID,
		Kind:        "click",
		Url:         link.Url,
		UserAgent:   truncate(r.UserAgent(), 255),
	}); err != nil {
		log.Error().Err(err).Msg("campaign: create click event failed")
	}
	if firstClick {
		if c, cerr := h.queries.GetCampaign(ctx, rec.CampaignID); cerr == nil {
			h.notifyEvent(ctx, rec, c, "click", link.Url)
		}
	}
	http.Redirect(w, r, link.Url, http.StatusFound)
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
