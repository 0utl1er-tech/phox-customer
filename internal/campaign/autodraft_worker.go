package campaign

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"connectrpc.com/connect"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/notify"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// AutoDraftWorker は「投函された Book からキャンペーン下書きを自動生成する」
// 常駐 worker (Phase 28f)。
//
// 28d の lakehouse パイプラインが Google Maps 由来のリードを Book
// (`GM_{業種}_{都道府県}_{YYYY-MM}_HPあり` / `_HPなし`) に自動投函する。
// この worker は 15 分毎に、有効な CampaignAutoDraft テンプレの
// book_name_pattern に LIKE 一致し、かつまだ自動下書きを作っていない Book
// (= Campaign.source_book_id に未登場) を見つけ、テンプレの文面で draft を
// 作り、Discord に「確認して開始してください」と通知する。
//
// **絶対に送信を開始しない**。status は draft のままで、StartCampaign は
// 常に人間が押す。
//
// 多重実行の安全性 (replicas=2) は二重防御:
//   - Redis リーダーロック (REDIS_ADDR 未設定の dev ではロックレス)
//   - Campaign.source_book_id の UNIQUE 制約 — 競合した INSERT は 23505 で
//     落ち、worker はそれを no-op として扱う
type AutoDraftWorker struct {
	queries  *db.Queries
	creator  *DraftCreator
	notifier notify.AutoDraftNotifier
	lock     *leaderLock
}

const (
	// autoDraftInterval は tick 間隔。投函は日次〜週次なので 15 分で十分。
	autoDraftInterval = 15 * time.Minute
	// autoDraftLockTTL は tick 中にロックが切れない長さ。
	autoDraftLockTTL = 5 * time.Minute
	// autoDraftLockKey は送信 worker とは別のリーダーロック。
	autoDraftLockKey = "phox:campaign:autodraft:lock"
	// autoDraftMaxBooksPerTick は 1 tick あたりの処理上限 (テンプレ毎)。
	// 一気に大量の下書きを作って人間を溺れさせないための安全弁。
	autoDraftMaxBooksPerTick = 10
)

// NewAutoDraftWorker は worker を組み立てる。rdb が nil ならロックレス
// (単 pod 前提の dev)。notifier が nil でも動く (通知だけ skip)。
func NewAutoDraftWorker(
	queries *db.Queries,
	dbPool *pgxpool.Pool,
	rdb *redis.Client,
	notifier notify.AutoDraftNotifier,
) *AutoDraftWorker {
	if queries == nil || dbPool == nil {
		return nil
	}
	host, _ := os.Hostname()
	return &AutoDraftWorker{
		queries:  queries,
		creator:  NewDraftCreator(queries, dbPool),
		notifier: notifier,
		lock:     newLeaderLock(rdb, autoDraftLockKey, host+"-"+uuid.NewString(), autoDraftLockTTL),
	}
}

// Run は 15 分毎の tick で自動下書き生成を回す (起動直後にも 1 回)。
// ctx cancel で正常終了する。エラーは tick 内で吸収し worker は落とさない。
func (w *AutoDraftWorker) Run(ctx context.Context) error {
	if w == nil {
		return nil
	}
	log.Info().Msg("campaign: auto-draft worker started")
	defer w.lock.release(context.Background())

	if w.lock.tryAcquireOrRenew(ctx) {
		w.tick(ctx)
	}

	ticker := time.NewTicker(autoDraftInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("campaign: auto-draft worker stopped")
			return nil
		case <-ticker.C:
			if !w.lock.tryAcquireOrRenew(ctx) {
				continue // 他 pod が leader
			}
			w.tick(ctx)
		}
	}
}

// tick は 1 回分の走査。有効テンプレを順に見て、未処理 Book に下書きを作る。
func (w *AutoDraftWorker) tick(ctx context.Context) {
	templates, err := w.queries.ListEnabledCampaignAutoDrafts(ctx)
	if err != nil {
		log.Error().Err(err).Msg("campaign auto-draft: list templates failed")
		return
	}
	if len(templates) == 0 {
		return // テンプレ未設定 = 何も起きない (既定)
	}
	created := 0
	for _, tpl := range templates {
		created += w.runTemplate(ctx, tpl)
	}
	if created > 0 {
		log.Info().Int("drafts", created).Msg("campaign auto-draft: drafts created")
	}
}

// runTemplate はテンプレ 1 本分の処理。作成できた下書きの本数を返す。
func (w *AutoDraftWorker) runTemplate(ctx context.Context, tpl db.CampaignAutoDraft) int {
	books, err := w.queries.ListUnclaimedBooksByNamePattern(ctx, db.ListUnclaimedBooksByNamePatternParams{
		Pattern:  tpl.BookNamePattern,
		MaxBooks: autoDraftMaxBooksPerTick,
	})
	if err != nil {
		log.Error().Err(err).
			Str("template", tpl.Name).
			Str("pattern", tpl.BookNamePattern).
			Msg("campaign auto-draft: list books failed")
		return 0
	}
	created := 0
	for _, book := range books {
		if w.createDraft(ctx, tpl, book) {
			created++
		}
	}
	return created
}

// createDraft は Book 1 冊から下書きを 1 本作り、通知する。
// 作れなかった場合 (権限不足・受信者 0 件・競合) は false を返す。
func (w *AutoDraftWorker) createDraft(ctx context.Context, tpl db.CampaignAutoDraft, book db.Book) bool {
	bookID := book.ID
	sched := DraftSchedule{
		SendStartHour:        tpl.SendStartHour,
		SendEndHour:          tpl.SendEndHour,
		SendDays:             tpl.SendDays,
		DailyCapPerMailbox:   tpl.DailyCapPerMailbox,
		MinIntervalSec:       tpl.MinIntervalSec,
		WarmupEnabled:        tpl.WarmupEnabled,
		BouncePauseThreshold: tpl.BouncePauseThreshold,
	}
	in := DraftInput{
		CompanyID:     tpl.CompanyID,
		CreatorUserID: tpl.CreatorUserID,
		Name:          book.Name,
		Subject:       tpl.Subject,
		Body:          tpl.Body,
		TrackOpens:    tpl.TrackOpens,
		TrackClicks:   tpl.TrackClicks,
		MailboxIDs:    tpl.MailboxIds,
		BookIDs:       []uuid.UUID{bookID},
		Schedule:      &sched,
		Sender: DraftSender{
			Org:     tpl.SenderOrg,
			Address: tpl.SenderAddress,
			Contact: tpl.SenderContact,
		},
		Followups:    DecodeFollowups(tpl.Followups),
		SourceBookID: &bookID,
	}

	res, err := w.creator.Create(ctx, in)
	if err != nil {
		w.logSkip(err, tpl, book)
		return false
	}

	if terr := w.queries.TouchCampaignAutoDraftCreated(ctx, tpl.ID); terr != nil {
		log.Warn().Err(terr).Str("template", tpl.Name).Msg("campaign auto-draft: touch last_created_at failed")
	}

	skipped := res.SkippedNoEmail + res.SkippedSuppressed + res.SkippedDuplicate + res.SkippedNoMX
	log.Info().
		Str("campaign_id", res.Campaign.ID.String()).
		Str("campaign", res.Campaign.Name).
		Str("book", book.Name).
		Str("template", tpl.Name).
		Int32("queued", res.Queued).
		Int32("skipped", skipped).
		Msg("campaign auto-draft: draft created (start は人間が押すこと)")

	if w.notifier != nil {
		w.notifier.NotifyCampaignAutoDraft(ctx, notify.AutoDraftInfo{
			CampaignID:     res.Campaign.ID,
			CampaignName:   res.Campaign.Name,
			BookName:       book.Name,
			TemplateName:   tpl.Name,
			RecipientCount: res.Queued,
			SkippedCount:   skipped,
		})
	}
	return true
}

// logSkip は下書きを作れなかった理由をログレベル分けして出す。
// 通知はしない (人間に見せるのは「作れた」ときだけ)。
func (w *AutoDraftWorker) logSkip(err error, tpl db.CampaignAutoDraft, book db.Book) {
	ev := log.Warn()
	msg := "campaign auto-draft: skipped"
	switch {
	case IsDuplicateSourceBook(err):
		// 他 pod / 直前の tick が同じ Book から作った = 正常な競合。
		ev = log.Debug()
		msg = "campaign auto-draft: already claimed by another worker"
	case connect.CodeOf(err) == connect.CodePermissionDenied:
		// creator_user_id が Book/Mailbox の権限を持っていない設定ミス。
		// 毎 tick 出るが、直さない限り下書きは作られないので出し続ける。
		msg = "campaign auto-draft: skipped — creator lacks permission (Book/Mailbox の権限を確認)"
	case connect.CodeOf(err) == connect.CodeInvalidArgument:
		// 送信可能な受信者が 0 件 (HPなし Book では正常)。
		msg = "campaign auto-draft: skipped — no sendable recipients"
	}
	ev.Err(err).
		Str("template", tpl.Name).
		Str("book", book.Name).
		Str("book_id", book.ID.String()).
		Str("creator_user_id", tpl.CreatorUserID).
		Msg(msg)
}

// autoDraftFollowupJSON は CampaignAutoDraft.followups (JSONB) の 1 要素。
type autoDraftFollowupJSON struct {
	WaitDays int32  `json:"wait_days"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
}

// DecodeFollowups は JSONB のフォローアップ配列を DraftFollowup に直す。
// 壊れた JSON は「フォローアップ無し」に倒す (下書き生成自体は止めない)。
func DecodeFollowups(raw []byte) []DraftFollowup {
	if len(raw) == 0 {
		return nil
	}
	var items []autoDraftFollowupJSON
	if err := json.Unmarshal(raw, &items); err != nil {
		log.Warn().Err(err).Msg("campaign auto-draft: invalid followups JSON — treated as empty")
		return nil
	}
	out := make([]DraftFollowup, 0, len(items))
	for _, it := range items {
		out = append(out, DraftFollowup{WaitDays: it.WaitDays, Subject: it.Subject, Body: it.Body})
	}
	return out
}

// EncodeFollowups は DraftFollowup 配列を JSONB 用のバイト列にする
// (RPC 側の保存で使う)。
func EncodeFollowups(fus []DraftFollowup) ([]byte, error) {
	items := make([]autoDraftFollowupJSON, 0, len(fus))
	for _, fu := range fus {
		items = append(items, autoDraftFollowupJSON{WaitDays: fu.WaitDays, Subject: fu.Subject, Body: fu.Body})
	}
	return json.Marshal(items)
}
