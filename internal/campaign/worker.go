package campaign

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	"github.com/0utl1er-tech/phox-customer/internal/mail"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const (
	tickInterval    = 30 * time.Second
	lockTTL         = 90 * time.Second
	staleClaimAfter = 15 * time.Minute
	mailboxCooldown = 10 * time.Minute
	maxAttempts     = 3
	lockKey         = "phox:campaign:sender:lock"
)

// Worker はキャンペーン送信ループ。MailboxIMAPWorker と同じ形:
// NewWorker が nil を返せば無効、Run(ctx) が ticker ループ。
//
// 送信は「leader のみ + per-recipient CAS claim」の二重防御で at-most-once。
// claim したまま落ちた行は janitor が failed に倒し、自動再送はしない
// (コールドメールでは二重送信の方が 1 通の欠落より害が大きい)。
type Worker struct {
	queries    *db.Queries
	sender     *mail.MailboxSender
	cipher     *crypto.Cipher
	tokenizer  *Tokenizer
	publicBase string // 例 https://phox-api.0utl1er.tech (末尾スラッシュ無し)
	lock       *leaderLock

	// leader は単一 pod なので in-memory で十分 (ロック喪失時はリセットされるが
	// daily cap は DB 起算なので安全側に働く)。
	nextAllowed   map[uuid.UUID]time.Time // mailbox 毎の次回送信可能時刻 (jitter 済み)
	cooldownUntil map[uuid.UUID]time.Time // SMTP transient エラー後のクールダウン
}

// NewWorker returns nil when prerequisites are missing (feature disabled).
// tokenizer / publicBase は配信停止 URL の生成に必須 — 特電法上、リンク無しの
// コールドメール送信は許可しない。
func NewWorker(
	queries *db.Queries,
	sender *mail.MailboxSender,
	cipher *crypto.Cipher,
	rdb *redis.Client,
	tokenizer *Tokenizer,
	publicBase string,
) *Worker {
	if sender == nil || cipher == nil {
		return nil
	}
	if tokenizer == nil || publicBase == "" {
		log.Warn().Msg("campaign: worker disabled — CAMPAIGN_TRACKING_KEY and PHOX_API_PUBLIC_BASE_URL are required (unsubscribe URL は法令上必須)")
		return nil
	}
	host, _ := os.Hostname()
	return &Worker{
		queries:       queries,
		sender:        sender,
		cipher:        cipher,
		tokenizer:     tokenizer,
		publicBase:    strings.TrimSuffix(publicBase, "/"),
		lock:          newLeaderLock(rdb, lockKey, host+"-"+uuid.NewString(), lockTTL),
		nextAllowed:   map[uuid.UUID]time.Time{},
		cooldownUntil: map[uuid.UUID]time.Time{},
	}
}

// Run blocks until ctx is done.
func (w *Worker) Run(ctx context.Context) error {
	log.Info().Msg("campaign: send worker started")
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	defer w.lock.release(context.Background())
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if !w.lock.tryAcquireOrRenew(ctx) {
				continue // 他 pod が leader
			}
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	now := time.Now()

	// janitor: claim したまま落ちた行を failed に倒す (自動再送しない)。
	if n, err := w.queries.FailStaleSendingRecipients(ctx,
		pgtype.Timestamptz{Time: now.Add(-staleClaimAfter), Valid: true}); err != nil {
		log.Error().Err(err).Msg("campaign: janitor failed")
	} else if n > 0 {
		log.Warn().Int64("count", n).Msg("campaign: stale sending recipients marked failed")
	}

	campaigns, err := w.queries.ListRunningCampaigns(ctx)
	if err != nil {
		log.Error().Err(err).Msg("campaign: list running campaigns failed")
		return
	}
	for _, c := range campaigns {
		// 完了判定は送信窓の外でも行う (窓の外で queued 0 になっても閉じたい)。
		if done, err := w.completeIfDrained(ctx, c); err != nil || done {
			continue
		}
		if !inSendWindow(c, now) {
			continue
		}
		w.sendOne(ctx, c, now)
	}
}

// completeIfDrained marks the campaign completed when nothing is left to send.
func (w *Worker) completeIfDrained(ctx context.Context, c db.Campaign) (bool, error) {
	remaining, err := w.queries.CountQueuedRecipients(ctx, c.ID)
	if err != nil {
		log.Error().Err(err).Str("campaign", c.ID.String()).Msg("campaign: count queued failed")
		return false, err
	}
	if remaining > 0 {
		return false, nil
	}
	if _, err := w.queries.SetCampaignStatus(ctx, db.SetCampaignStatusParams{ID: c.ID, Status: "completed"}); err != nil {
		log.Error().Err(err).Str("campaign", c.ID.String()).Msg("campaign: mark completed failed")
		return false, err
	}
	log.Info().Str("campaign", c.ID.String()).Str("name", c.Name).Msg("campaign: completed")
	return true, nil
}

// inSendWindow は JST の営業時間帯 + 曜日 bitmask (Mon=1..Sun=64、0 は平日扱い)。
func inSendWindow(c db.Campaign, now time.Time) bool {
	nowJST := now.In(jst)
	h := int32(nowJST.Hour())
	if h < c.SendStartHour || h >= c.SendEndHour {
		return false
	}
	days := c.SendDays
	if days == 0 {
		days = 31 // 平日
	}
	// time.Weekday: Sunday=0..Saturday=6 → Mon=bit0..Sun=bit6 に写像
	bit := int32(1) << ((int(nowJST.Weekday()) + 6) % 7)
	return days&bit != 0
}

// sendOne は 1 tick につきキャンペーンあたり最大 1 通送る。
// tick 30 秒 × mailbox 毎の next_allowed ゲートで自然にペーシングされる。
func (w *Worker) sendOne(ctx context.Context, c db.Campaign, now time.Time) {
	mb, ok := w.pickMailbox(ctx, c, now)
	if !ok {
		return // 予算切れ / クールダウン中 / インターバル未達
	}

	rec, err := w.queries.ClaimCampaignRecipient(ctx, db.ClaimCampaignRecipientParams{
		CampaignID: c.ID,
		MailboxID:  pgtype.UUID{Bytes: mb.ID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return // queued 無し (完了判定は次 tick の completeIfDrained)
	}
	if err != nil {
		log.Error().Err(err).Str("campaign", c.ID.String()).Msg("campaign: claim failed")
		return
	}

	// スナップショット後にサプレッションへ入った可能性があるため送信直前に再チェック。
	if _, serr := w.queries.GetSuppressionByEmail(ctx, db.GetSuppressionByEmailParams{
		CompanyID: c.CompanyID, Lower: rec.Email,
	}); serr == nil {
		_ = w.queries.MarkRecipientSkipped(ctx, db.MarkRecipientSkippedParams{
			ID: rec.ID, Error: "suppressed after snapshot",
		})
		return
	}

	customer, err := w.queries.GetCustomer(ctx, rec.CustomerID)
	if err != nil {
		_ = w.queries.MarkRecipientFailed(ctx, db.MarkRecipientFailedParams{
			ID: rec.ID, Error: "customer lookup failed: " + err.Error(),
		})
		return
	}
	password, err := w.cipher.DecryptString(mb.PasswordEnc)
	if err != nil {
		// 資格情報の問題は receiver 単位でなく mailbox 単位 — requeue して cooldown。
		w.requeueTransient(ctx, rec, mb.ID, now, "decrypt mailbox password: "+err.Error())
		return
	}

	messageID := fmt.Sprintf("cmp-%s@%s", rec.ID.String(), domainOf(mb.Address))
	unsubURL := w.publicBase + "/u/" + w.tokenizer.Token(KindUnsubscribe, rec.ID, 0)
	vars := map[string]string{
		"customer_name":        customer.Name,
		"customer_corporation": customer.Corporation,
		"customer_mail":        rec.Email,
		"customer_phone":       customer.Phone,
		"sender_name":          mb.DisplayName,
		"sender_mail":          mb.Address,
		"today":                TodayJST(now),
	}
	subject := Render(c.Subject, vars)
	body := RenderBody(c.Body, vars, SenderInfo{
		Org: c.SenderOrg, Address: c.SenderAddress, Contact: c.SenderContact,
	}, unsubURL)
	headers := map[string]string{
		"List-Unsubscribe":      "<" + unsubURL + ">",
		"List-Unsubscribe-Post": "List-Unsubscribe=One-Click", // RFC 8058
	}

	// Phase 27b: トラッキング有効時のみ HTML alt パートを付ける
	// (両方 OFF なら純 text/plain — 到達率最優先の設定)。
	htmlBody := w.buildTrackedHTML(ctx, c, rec, body, unsubURL)

	err = w.sender.SendAsWithHeaders(ctx,
		mb.SmtpUsername, password,
		mb.Address, mb.DisplayName,
		rec.Email, nil,
		subject, body, messageID, headers, htmlBody)
	if err != nil {
		// SMTP エラーの transient/permanent 判別は不安定なので試行回数で打ち切る。
		if rec.Attempts+1 >= maxAttempts {
			_ = w.queries.MarkRecipientFailed(ctx, db.MarkRecipientFailedParams{
				ID: rec.ID, Error: fmt.Sprintf("send failed (attempt %d): %v", rec.Attempts+1, err),
			})
		} else {
			w.requeueTransient(ctx, rec, mb.ID, now, err.Error())
		}
		log.Warn().Err(err).Str("campaign", c.ID.String()).Str("to", rec.Email).Msg("campaign: send failed")
		return
	}

	if merr := w.queries.MarkRecipientSent(ctx, db.MarkRecipientSentParams{
		ID: rec.ID, MessageID: pgtype.Text{String: messageID, Valid: true},
	}); merr != nil {
		// 送信済みなのに記録に失敗 — janitor が stale-claim で failed に倒すため
		// 二重送信にはならないが、必ずログに残す。
		log.Error().Err(merr).Str("recipient", rec.ID.String()).Msg("campaign: mark sent failed AFTER smtp accept")
		return
	}

	// 顧客タイムラインにも載せる。失敗しても送信自体は成立しているのでログのみ。
	if _, aerr := w.queries.CreateActivity(ctx, db.CreateActivityParams{
		ID:         uuid.New(),
		CustomerID: rec.CustomerID,
		ContactID:  pgtype.UUID{Valid: false},
		Type:       "email_sent",
		UserID:     c.CreatedBy,
		StatusID:   pgtype.UUID{Valid: false},
		MailFrom:   pgtype.Text{String: mb.Address, Valid: true},
		MailTo:     pgtype.Text{String: rec.Email, Valid: true},
		MailCc:     pgtype.Text{Valid: false},
		Subject:    pgtype.Text{String: subject, Valid: true},
		Body:       pgtype.Text{String: body, Valid: true},
		MessageID:  pgtype.Text{String: messageID, Valid: true},
		OccurredAt: now,
		MailboxID:  pgtype.UUID{Bytes: mb.ID, Valid: true},
	}); aerr != nil {
		log.Error().Err(aerr).Str("recipient", rec.ID.String()).Msg("campaign: activity insert failed")
	}

	// jitter ±50%: min_interval_sec * (0.5 + rand)
	interval := time.Duration(float64(c.MinIntervalSec)*(0.5+rand.Float64())) * time.Second
	w.nextAllowed[mb.ID] = now.Add(interval)
	log.Info().
		Str("campaign", c.ID.String()).
		Str("mailbox", mb.Address).
		Str("to", rec.Email).
		Msg("campaign: sent")
}

// buildTrackedHTML はトラッキング設定に応じた HTML alt パートを組み立てる。
// クリック計測: 本文 URL を CampaignLink に登録し (URL は DB 持ち = open
// redirect 無し)、HTML 側のリンクだけ /t/c/{token} に書き換える。配信停止 URL
// (自 publicBase 配下) は二重トラッキングしない。開封計測: 1×1 ピクセル。
// 両方 OFF なら空文字 (text/plain のみ)。
func (w *Worker) buildTrackedHTML(ctx context.Context, c db.Campaign, rec db.CampaignRecipient, body, unsubURL string) string {
	if !c.TrackOpens && !c.TrackClicks {
		return ""
	}
	var linkURL func(string) (string, bool)
	if c.TrackClicks {
		urlToIdx := map[string]int32{}
		links, err := w.queries.ListCampaignLinks(ctx, c.ID)
		if err != nil {
			log.Error().Err(err).Str("campaign", c.ID.String()).Msg("campaign: list links failed — click tracking skipped")
		} else {
			for _, l := range links {
				urlToIdx[l.Url] = l.Idx
			}
			next := int32(len(links))
			linkURL = func(raw string) (string, bool) {
				if raw == unsubURL || strings.HasPrefix(raw, w.publicBase+"/u/") {
					return "", false // 配信停止リンクはそのまま
				}
				idx, ok := urlToIdx[raw]
				if !ok {
					// プレースホルダ入り URL は受信者毎に変わり得るので都度追加。
					// worker は単一 leader なので競合しない。
					idx = next
					if cerr := w.queries.CreateCampaignLink(ctx, db.CreateCampaignLinkParams{
						CampaignID: c.ID, Idx: idx, Url: raw,
					}); cerr != nil {
						log.Error().Err(cerr).Msg("campaign: create link failed")
						return "", false
					}
					urlToIdx[raw] = idx
					next++
				}
				if idx > 65535 {
					return "", false // token の idx は uint16
				}
				return w.publicBase + "/t/c/" + w.tokenizer.Token(KindClick, rec.ID, uint16(idx)), true
			}
		}
	}
	pixelURL := ""
	if c.TrackOpens {
		pixelURL = w.publicBase + "/t/o/" + w.tokenizer.Token(KindOpen, rec.ID, 0)
	}
	return BuildHTMLBody(body, linkURL, pixelURL)
}

func (w *Worker) requeueTransient(ctx context.Context, rec db.CampaignRecipient, mailboxID uuid.UUID, now time.Time, msg string) {
	_ = w.queries.RequeueRecipient(ctx, db.RequeueRecipientParams{ID: rec.ID, Error: msg})
	w.cooldownUntil[mailboxID] = now.Add(mailboxCooldown)
}

// pickMailbox はプール内で「予算あり・cooldown 外・next_allowed 到達済み」の
// mailbox のうち next_allowed が最も古いものを選ぶ — ローテーションは自然に
// 発生する。
func (w *Worker) pickMailbox(ctx context.Context, c db.Campaign, now time.Time) (db.Mailbox, bool) {
	mailboxes, err := w.queries.ListCampaignMailboxes(ctx, c.ID)
	if err != nil || len(mailboxes) == 0 {
		if err != nil {
			log.Error().Err(err).Str("campaign", c.ID.String()).Msg("campaign: list mailboxes failed")
		}
		return db.Mailbox{}, false
	}
	midnight := JSTMidnight(now)
	var best db.Mailbox
	var bestNext time.Time
	found := false
	for _, mb := range mailboxes {
		if now.Before(w.cooldownUntil[mb.ID]) {
			continue
		}
		next := w.nextAllowed[mb.ID]
		if now.Before(next) {
			continue
		}
		sentToday, cerr := w.queries.CountSentSinceByMailbox(ctx, db.CountSentSinceByMailboxParams{
			MailboxID: pgtype.UUID{Bytes: mb.ID, Valid: true},
			SentAt:    pgtype.Timestamptz{Time: midnight, Valid: true},
		})
		if cerr != nil {
			continue
		}
		if sentToday >= int64(w.effectiveCap(c, now)) {
			continue
		}
		if !found || next.Before(bestNext) {
			best, bestNext, found = mb, next, true
		}
	}
	return best, found
}

// effectiveCap: warmup 有効時は min(cap, 20 + 10*開始からの日数) で漸増。
func (w *Worker) effectiveCap(c db.Campaign, now time.Time) int32 {
	cap := c.DailyCapPerMailbox
	if cap <= 0 {
		cap = 100
	}
	if !c.WarmupEnabled || !c.StartedAt.Valid {
		return cap
	}
	days := int32(now.Sub(c.StartedAt.Time).Hours() / 24)
	ramp := 20 + 10*days
	if ramp < cap {
		return ramp
	}
	return cap
}

// JSTMidnight は JST の当日 0 時 (daily cap の起算点)。
func JSTMidnight(now time.Time) time.Time {
	n := now.In(jst)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, jst)
}

func domainOf(address string) string {
	if i := strings.LastIndex(address, "@"); i >= 0 && i+1 < len(address) {
		return address[i+1:]
	}
	return "phox.local"
}
