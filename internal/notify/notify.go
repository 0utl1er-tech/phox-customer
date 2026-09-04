// Package notify はキャンペーン反響イベントの外部通知 (Phase 27h)。
//
// 会社の管理者設定 (Company.notify_webhook_url / notify_events) に従い、
// 反響イベント (返信/クリック/配信停止/バウンス/開封) の「受信者×種別ごとの
// 初回」を Discord Webhook に embed で POST する。
//
// 設計方針:
//   - 通知はベストエフォート。イベント処理 (トラッキング HTTP ハンドラ /
//     IMAP 取込) をブロックしないよう goroutine + タイムアウトで送り、
//     失敗しても warn ログのみ (リトライしない)。
//   - 会社設定はイベントごとに DB から引く (反響は低頻度なので N+1 許容)。
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// KnownEvents は notify_events に指定できるイベント種別 (canonical 順)。
// autodraft (Phase 28f) は反響ではなく「下書きが自動生成された」の運用通知。
// Company.notify_events の既定値 ('reply') は変えていないのでオプトイン。
var KnownEvents = []string{"reply", "click", "unsubscribe", "bounce", "open", "autodraft"}

// EventAutoDraft は自動下書き生成の通知種別 (Phase 28f)。
const EventAutoDraft = "autodraft"

// notifyTimeout は Webhook POST の上限。イベント処理はブロックしないが、
// goroutine を無限に生かさないための保険。
const notifyTimeout = 5 * time.Second

// CampaignEventInfo は通知 1 件分の材料。呼び出し側 (トラッキングハンドラ /
// メール取込) がイベント時に組み立てる。
type CampaignEventInfo struct {
	Kind         string // reply | click | unsubscribe | bounce | open
	CampaignID   uuid.UUID
	CampaignName string
	CustomerName string
	Corporation  string
	Email        string
	URL          string // click のみ: クリックされたリンク先
}

// AutoDraftInfo は「キャンペーン下書きを自動生成した」通知 1 件分の材料
// (Phase 28f)。反響イベントではなく運用通知なので専用の型にする。
type AutoDraftInfo struct {
	CampaignID     uuid.UUID
	CampaignName   string
	BookName       string // 由来 Book (例 GM_中古車販売店_埼玉県_2026-09_HPあり)
	TemplateName   string // 使った自動下書きテンプレの管理用ラベル
	RecipientCount int32  // 送信対象 (queued)
	SkippedCount   int32  // 除外 (メール無し/サプレッション/重複/MX 無し)
}

// Notifier はキャンペーン反響通知の注入点。実装は DiscordNotifier のみ。
type Notifier interface {
	// NotifyCampaignEvent は非同期に通知を送る (即 return し、失敗は warn ログのみ)。
	NotifyCampaignEvent(ctx context.Context, ev CampaignEventInfo)
}

// AutoDraftNotifier は自動下書き worker が使う通知の注入点 (Phase 28f)。
type AutoDraftNotifier interface {
	NotifyCampaignAutoDraft(ctx context.Context, info AutoDraftInfo)
}

// ValidateWebhookURL は notify_webhook_url の入力検証。空 = 通知無効で常に OK。
//
// Discord の Webhook (https://discord.com/api/webhooks/...) が本来の用途だが、
// staging での実測検証 (cluster 内 echo サービス) を可能にするため任意の
// http(s) URL を許可する。SSRF になり得るが、設定できるのは owner のみ
// (UpdateSettings の RBAC) なので許容する。
func ValidateWebhookURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("webhook URL は http(s):// で始まる URL を指定してください")
	}
	return nil
}

// NormalizeEvents はカンマ区切りのイベントリストを検証して canonical 形式
// (KnownEvents 順・重複除去) に直す。空文字は「全 OFF」で OK。
func NormalizeEvents(csv string) (string, error) {
	enabled := map[string]bool{}
	for _, raw := range strings.Split(csv, ",") {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		known := false
		for _, k := range KnownEvents {
			if e == k {
				known = true
				break
			}
		}
		if !known {
			return "", fmt.Errorf("不明な通知イベントです: %q (有効値: %s)", e, strings.Join(KnownEvents, ","))
		}
		enabled[e] = true
	}
	out := make([]string, 0, len(enabled))
	for _, k := range KnownEvents {
		if enabled[k] {
			out = append(out, k)
		}
	}
	return strings.Join(out, ","), nil
}

// EventEnabled は notify_events (カンマ区切り) に kind が含まれるか。
func EventEnabled(csv, kind string) bool {
	for _, raw := range strings.Split(csv, ",") {
		if strings.TrimSpace(raw) == kind {
			return true
		}
	}
	return false
}

// DiscordNotifier は Company.notify_webhook_url へ Discord embed を POST する。
type DiscordNotifier struct {
	queries *db.Queries
	baseURL string // cfg.PhoxBaseURL — キャンペーン詳細へのリンク用
	client  *http.Client
}

func NewDiscordNotifier(queries *db.Queries, baseURL string) *DiscordNotifier {
	return &DiscordNotifier{
		queries: queries,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: notifyTimeout},
	}
}

// NotifyCampaignEvent は goroutine で送信し即 return する。呼び出し元の
// リクエスト ctx がすぐ終わっても送信は続くよう WithoutCancel で切り離す。
func (n *DiscordNotifier) NotifyCampaignEvent(ctx context.Context, ev CampaignEventInfo) {
	if n == nil {
		return
	}
	go func() {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), notifyTimeout)
		defer cancel()
		if err := n.send(sctx, ev); err != nil {
			log.Warn().Err(err).
				Str("kind", ev.Kind).
				Str("campaign_id", ev.CampaignID.String()).
				Msg("notify: campaign event webhook failed")
		}
	}()
}

// NotifyCampaignAutoDraft は「下書きを作成しました」通知を非同期に送る
// (Phase 28f)。反響通知と同じくベストエフォート (失敗は warn ログのみ)。
func (n *DiscordNotifier) NotifyCampaignAutoDraft(ctx context.Context, info AutoDraftInfo) {
	if n == nil {
		return
	}
	go func() {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), notifyTimeout)
		defer cancel()
		if err := n.sendAutoDraft(sctx, info); err != nil {
			log.Warn().Err(err).
				Str("campaign_id", info.CampaignID.String()).
				Msg("notify: campaign auto-draft webhook failed")
		}
	}()
}

// sendAutoDraft は会社設定 (webhook + notify_events に autodraft) が有効なら
// embed を POST する。無効なら no-op (エラーではない)。
func (n *DiscordNotifier) sendAutoDraft(ctx context.Context, info AutoDraftInfo) error {
	c, err := n.queries.GetCampaign(ctx, info.CampaignID)
	if err != nil {
		return fmt.Errorf("get campaign: %w", err)
	}
	company, err := n.queries.GetCompany(ctx, c.CompanyID)
	if err != nil {
		return fmt.Errorf("get company: %w", err)
	}
	if company.NotifyWebhookUrl == "" || !EventEnabled(company.NotifyEvents, EventAutoDraft) {
		return nil // 通知無効 — 正常系
	}
	return n.post(ctx, company.NotifyWebhookUrl, n.buildAutoDraftPayload(info))
}

func (n *DiscordNotifier) buildAutoDraftPayload(info AutoDraftInfo) discordPayload {
	return discordPayload{Embeds: []discordEmbed{{
		Title: "📝 キャンペーン下書きを作成しました",
		URL:   n.campaignURL(info.CampaignID),
		Color: 0x5865F2,
		Fields: []discordEmbedField{
			{Name: "キャンペーン", Value: orDash(info.CampaignName)},
			{Name: "由来 Book", Value: orDash(info.BookName)},
			{Name: "テンプレート", Value: orDash(info.TemplateName), Inline: true},
			{Name: "送信対象", Value: fmt.Sprintf("%d 件 (除外 %d 件)", info.RecipientCount, info.SkippedCount), Inline: true},
			{Name: "次のアクション", Value: "内容を確認して開始してください (自動では送信しません)"},
		},
	}}}
}

// send は会社設定を引いて有効なら embed を POST する (無効なら no-op)。
// 会社は campaign → company_id で解決する (単一テナント運用でも将来の
// マルチテナント化に耐える形)。
func (n *DiscordNotifier) send(ctx context.Context, ev CampaignEventInfo) error {
	c, err := n.queries.GetCampaign(ctx, ev.CampaignID)
	if err != nil {
		return fmt.Errorf("get campaign: %w", err)
	}
	company, err := n.queries.GetCompany(ctx, c.CompanyID)
	if err != nil {
		return fmt.Errorf("get company: %w", err)
	}
	if company.NotifyWebhookUrl == "" || !EventEnabled(company.NotifyEvents, ev.Kind) {
		return nil // 通知無効 — 正常系
	}

	return n.post(ctx, company.NotifyWebhookUrl, n.buildPayload(ev))
}

// post は embed payload を Webhook へ POST する (反響/自動下書き共通)。
func (n *DiscordNotifier) post(ctx context.Context, webhookURL string, body discordPayload) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// eventTitles / eventColors は Discord embed の見た目 (kind ごと)。
var eventTitles = map[string]string{
	"reply":       "💬 返信",
	"click":       "🖱️ クリック",
	"unsubscribe": "🚫 配信停止",
	"bounce":      "⚠️ バウンス",
	"open":        "👀 開封",
}

var eventColors = map[string]int{
	"reply":       0x57F287, // green
	"click":       0x5865F2, // blurple
	"unsubscribe": 0x99AAB5, // gray
	"bounce":      0xED4245, // red
	"open":        0xFEE75C, // yellow
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type discordEmbed struct {
	Title  string              `json:"title"`
	URL    string              `json:"url,omitempty"`
	Color  int                 `json:"color,omitempty"`
	Fields []discordEmbedField `json:"fields"`
}

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

func (n *DiscordNotifier) buildPayload(ev CampaignEventInfo) discordPayload {
	title := eventTitles[ev.Kind]
	if title == "" {
		title = ev.Kind
	}
	customer := ev.CustomerName
	if ev.Corporation != "" {
		customer = fmt.Sprintf("%s (%s)", ev.CustomerName, ev.Corporation)
	}
	if customer == "" {
		customer = "(不明)"
	}
	fields := []discordEmbedField{
		{Name: "顧客", Value: customer, Inline: true},
		{Name: "メールアドレス", Value: orDash(ev.Email), Inline: true},
		{Name: "キャンペーン", Value: orDash(ev.CampaignName)},
	}
	if ev.Kind == "click" && ev.URL != "" {
		fields = append(fields, discordEmbedField{Name: "クリックURL", Value: ev.URL})
	}
	return discordPayload{Embeds: []discordEmbed{{
		Title:  title,
		URL:    n.campaignURL(ev.CampaignID),
		Color:  eventColors[ev.Kind],
		Fields: fields,
	}}}
}

func (n *DiscordNotifier) campaignURL(id uuid.UUID) string {
	if n.baseURL == "" {
		return ""
	}
	return n.baseURL + "/campaigns/" + id.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
