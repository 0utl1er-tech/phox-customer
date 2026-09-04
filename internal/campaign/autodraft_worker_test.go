package campaign

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/notify"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// autodraft_worker_test.go は Phase 28f の自動下書き worker の DB 実測テスト。
//
// TEST_DB_SOURCE に migrate 済みの Postgres を指すと走る (未設定・接続不可なら
// SetupTestDB が skip する)。staging CNPG に使い捨て DB を作って migrate →
// テスト → DROP する運用を想定。
//
// 検証項目:
//   - パターン一致 Book → draft 作成 (status=draft のまま、source_book_id 記録)
//   - 同一 Book で二度作らない (tick を 2 回回しても 1 本)
//   - enabled=false は無反応
//   - パターン不一致は無反応
//   - creator が Book 権限を持たない場合はスキップ (通知もしない)

// fakeAutoDraftNotifier は通知の呼び出しを記録する。
type fakeAutoDraftNotifier struct {
	mu    sync.Mutex
	calls []notify.AutoDraftInfo
}

func (f *fakeAutoDraftNotifier) NotifyCampaignAutoDraft(_ context.Context, info notify.AutoDraftInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, info)
}

func (f *fakeAutoDraftNotifier) snapshot() []notify.AutoDraftInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]notify.AutoDraftInfo(nil), f.calls...)
}

// autoDraftFixture はテスト 1 本分の隔離された世界 (会社/ユーザー/
// メールボックス/Book/顧客)。Book 名とパターンにはテスト毎のユニークな
// prefix を付け、同じ DB を共有する他テストと干渉しないようにする。
type autoDraftFixture struct {
	pool     *pgxpool.Pool
	queries  *db.Queries
	prefix   string
	company  uuid.UUID
	userID   string
	mailbox  uuid.UUID
	notifier *fakeAutoDraftNotifier
	worker   *AutoDraftWorker
}

// mxTestDomain は MX 検証で DNS を引かせないためのテスト用ドメイン。
// DomainHealth キャッシュに has_mx=true を仕込んでおく。
const mxTestDomain = "autodraft-test.invalid"

func newAutoDraftFixture(t *testing.T) *autoDraftFixture {
	t.Helper()
	ctx := context.Background()
	pool, queries := testutil.SetupTestDB(t)

	prefix := "AD" + strings.ToUpper(uuid.NewString()[:8])

	companyID := uuid.New()
	_, err := queries.CreateCompany(ctx, db.CreateCompanyParams{ID: companyID, Name: "autodraft-" + prefix})
	require.NoError(t, err)

	userID := uuid.NewString()
	_, err = queries.CreateUser(ctx, db.CreateUserParams{
		ID: userID, CompanyID: companyID, Name: "autodraft-user",
	})
	require.NoError(t, err)

	mailboxID := uuid.New()
	_, err = queries.CreateMailbox(ctx, db.CreateMailboxParams{
		ID:           mailboxID,
		CompanyID:    companyID,
		Address:      "sales-" + strings.ToLower(prefix) + "@" + mxTestDomain,
		SmtpUsername: "sales-" + strings.ToLower(prefix),
		PasswordEnc:  []byte("enc"),
		Active:       true,
	})
	require.NoError(t, err)
	_, err = queries.CreateMailboxPermit(ctx, db.CreateMailboxPermitParams{
		ID: uuid.New(), MailboxID: mailboxID, UserID: userID, Role: db.RoleOwner,
	})
	require.NoError(t, err)

	// MX 検証で外部 DNS を引かないようキャッシュを仕込む。
	require.NoError(t, queries.UpsertDomainHealth(ctx, db.UpsertDomainHealthParams{
		Lower: mxTestDomain, HasMx: true, MxHost: "mx." + mxTestDomain,
	}))

	notifier := &fakeAutoDraftNotifier{}
	f := &autoDraftFixture{
		pool: pool, queries: queries, prefix: prefix,
		company: companyID, userID: userID, mailbox: mailboxID,
		notifier: notifier,
		// rdb=nil → ロックレス (単 pod 相当)。
		worker: NewAutoDraftWorker(queries, pool, nil, notifier),
	}
	t.Cleanup(func() { f.cleanup(t) })
	return f
}

// cleanup はこのフィクスチャが作った行を消す (同じ DB を使い回すため)。
func (f *autoDraftFixture) cleanup(t *testing.T) {
	ctx := context.Background()
	// Campaign / CampaignAutoDraft は company_id、Book は名前 prefix で消す。
	// Customer / Permit は Book の CASCADE ではないので明示的に消す。
	_, _ = f.pool.Exec(ctx, `DELETE FROM "CampaignAutoDraft" WHERE company_id = $1`, f.company)
	_, _ = f.pool.Exec(ctx, `DELETE FROM "Campaign" WHERE company_id = $1`, f.company)
	_, _ = f.pool.Exec(ctx, `DELETE FROM "Customer" WHERE book_id IN (SELECT id FROM "Book" WHERE name LIKE $1)`, f.prefix+"%")
	_, _ = f.pool.Exec(ctx, `DELETE FROM "Permit" WHERE book_id IN (SELECT id FROM "Book" WHERE name LIKE $1)`, f.prefix+"%")
	_, _ = f.pool.Exec(ctx, `DELETE FROM "Book" WHERE name LIKE $1`, f.prefix+"%")
	_, _ = f.pool.Exec(ctx, `DELETE FROM "Company" WHERE id = $1`, f.company)
}

// createBook は Book + (grantPermit なら) creator の editor Permit + 顧客 n 件。
func (f *autoDraftFixture) createBook(t *testing.T, suffix string, customers int, grantPermit bool) db.Book {
	t.Helper()
	ctx := context.Background()
	book, err := f.queries.CreateBook(ctx, db.CreateBookParams{
		ID: uuid.New(), Name: f.prefix + "_" + suffix,
	})
	require.NoError(t, err)
	if grantPermit {
		_, err = f.queries.CreatePermit(ctx, db.CreatePermitParams{
			ID: uuid.New(), BookID: book.ID, UserID: f.userID, Role: db.RoleEditor,
		})
		require.NoError(t, err)
	}
	for i := 0; i < customers; i++ {
		_, err := f.queries.CreateCustomer(ctx, db.CreateCustomerParams{
			ID:     uuid.New(),
			BookID: book.ID,
			Name:   "テスト顧客",
			Mail:   uuid.NewString()[:8] + "@" + mxTestDomain,
		})
		require.NoError(t, err)
	}
	return book
}

// createTemplate は自動下書きテンプレを 1 本作る。
func (f *autoDraftFixture) createTemplate(t *testing.T, pattern string, enabled bool) db.CampaignAutoDraft {
	t.Helper()
	followups, err := EncodeFollowups([]DraftFollowup{{WaitDays: 3, Subject: "", Body: "その後いかがでしょうか"}})
	require.NoError(t, err)
	sched := DefaultSchedule()
	ad, err := f.queries.CreateCampaignAutoDraft(context.Background(), db.CreateCampaignAutoDraftParams{
		ID:                   uuid.New(),
		CompanyID:            f.company,
		Name:                 "テンプレ " + pattern,
		Enabled:              enabled,
		BookNamePattern:      pattern,
		Subject:              "ホームページのご提案",
		Body:                 "{{customer_name}} 様\n\nご提案です。",
		Followups:            followups,
		MailboxIds:           []uuid.UUID{f.mailbox},
		TrackOpens:           true,
		TrackClicks:          true,
		SendStartHour:        sched.SendStartHour,
		SendEndHour:          sched.SendEndHour,
		SendDays:             sched.SendDays,
		DailyCapPerMailbox:   sched.DailyCapPerMailbox,
		MinIntervalSec:       sched.MinIntervalSec,
		WarmupEnabled:        sched.WarmupEnabled,
		BouncePauseThreshold: sched.BouncePauseThreshold,
		SenderOrg:            "0UTL1ER株式会社",
		SenderAddress:        "埼玉県...",
		SenderContact:        "info@example.com",
		CreatorUserID:        f.userID,
	})
	require.NoError(t, err)
	return ad
}

// campaignsForBook は source_book_id がその Book の Campaign を返す。
func (f *autoDraftFixture) campaignsForBook(t *testing.T, bookID uuid.UUID) []db.Campaign {
	t.Helper()
	rows, err := f.pool.Query(context.Background(),
		`SELECT id, name, status, source_book_id FROM "Campaign" WHERE source_book_id = $1`, bookID)
	require.NoError(t, err)
	defer rows.Close()
	var out []db.Campaign
	for rows.Next() {
		var c db.Campaign
		require.NoError(t, rows.Scan(&c.ID, &c.Name, &c.Status, &c.SourceBookID))
		out = append(out, c)
	}
	return out
}

func (f *autoDraftFixture) recipientCount(t *testing.T, campaignID uuid.UUID, status string) int {
	t.Helper()
	var n int
	err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "CampaignRecipient" WHERE campaign_id = $1 AND status = $2`,
		campaignID, status).Scan(&n)
	require.NoError(t, err)
	return n
}

// パターン一致 Book から draft が作られ、二度目の tick では作られないこと。
func TestAutoDraftWorker_CreatesDraftOnceAndOnlyOnce(t *testing.T) {
	f := newAutoDraftFixture(t)
	ctx := context.Background()

	book := f.createBook(t, "中古車販売店_埼玉県_2026-09_HPあり", 3, true)
	tpl := f.createTemplate(t, f.prefix+`\_%\_HPあり`, true)

	f.worker.tick(ctx)

	campaigns := f.campaignsForBook(t, book.ID)
	require.Len(t, campaigns, 1, "パターン一致 Book から下書きが 1 本作られる")
	c := campaigns[0]
	require.Equal(t, "draft", c.Status, "自動生成は draft のまま — 送信開始は人間")
	require.Equal(t, book.Name, c.Name)
	require.Equal(t, 3, f.recipientCount(t, c.ID, "queued"), "Book 内の顧客数と一致")

	// 通知は 1 回だけ、内容は Book 名 + 受信者数。
	calls := f.notifier.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, book.Name, calls[0].BookName)
	require.Equal(t, int32(3), calls[0].RecipientCount)
	require.Equal(t, tpl.Name, calls[0].TemplateName)

	// フォローアップもテンプレから引き継がれる。
	var steps int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT count(*) FROM "CampaignStep" WHERE campaign_id = $1`, c.ID).Scan(&steps))
	require.Equal(t, 1, steps)

	// last_created_at が記録される (UI の「最終作成日」)。
	updated, err := f.queries.GetCampaignAutoDraft(ctx, tpl.ID)
	require.NoError(t, err)
	require.True(t, updated.LastCreatedAt.Valid)

	// ── 二重作成防止: もう一度 tick しても増えない ──
	f.worker.tick(ctx)
	f.worker.tick(ctx)
	require.Len(t, f.campaignsForBook(t, book.ID), 1, "同じ Book から二度作らない (source_book_id)")
	require.Len(t, f.notifier.snapshot(), 1, "二度目は通知もしない")
}

// enabled=false のテンプレは無反応。
func TestAutoDraftWorker_DisabledTemplateDoesNothing(t *testing.T) {
	f := newAutoDraftFixture(t)
	book := f.createBook(t, "美容室_埼玉県_2026-09_HPあり", 2, true)
	f.createTemplate(t, f.prefix+`\_%\_HPあり`, false)

	f.worker.tick(context.Background())

	require.Empty(t, f.campaignsForBook(t, book.ID), "無効テンプレでは何も起きない")
	require.Empty(t, f.notifier.snapshot())
}

// パターンに一致しない Book は対象外。
func TestAutoDraftWorker_PatternMismatchDoesNothing(t *testing.T) {
	f := newAutoDraftFixture(t)
	book := f.createBook(t, "美容室_埼玉県_2026-09_HPなし", 2, true)
	// HPあり 用のテンプレのみ有効 → HPなし Book は拾わない。
	f.createTemplate(t, f.prefix+`\_%\_HPあり`, true)

	f.worker.tick(context.Background())

	require.Empty(t, f.campaignsForBook(t, book.ID), "パターン不一致は無反応")
	require.Empty(t, f.notifier.snapshot())
}

// creator が Book の権限を持たない場合はスキップ (下書きも通知も作らない)。
func TestAutoDraftWorker_PermissionDeniedSkips(t *testing.T) {
	f := newAutoDraftFixture(t)
	book := f.createBook(t, "整体院_埼玉県_2026-09_HPあり", 2, false) // Permit を与えない
	f.createTemplate(t, f.prefix+`\_%\_HPあり`, true)

	f.worker.tick(context.Background())

	require.Empty(t, f.campaignsForBook(t, book.ID), "権限不足の Book はスキップ")
	require.Empty(t, f.notifier.snapshot(), "スキップ時は通知しない")
}

// HPあり / HPなし で別テンプレが引ける (テンプレは複数持てる)。
func TestAutoDraftWorker_MultipleTemplatesByPattern(t *testing.T) {
	f := newAutoDraftFixture(t)
	ctx := context.Background()

	withHP := f.createBook(t, "中古車販売店_埼玉県_2026-09_HPあり", 2, true)
	withoutHP := f.createBook(t, "中古車販売店_埼玉県_2026-09_HPなし", 1, true)
	f.createTemplate(t, f.prefix+`\_%\_HPあり`, true)
	f.createTemplate(t, f.prefix+`\_%\_HPなし`, true)

	f.worker.tick(ctx)

	require.Len(t, f.campaignsForBook(t, withHP.ID), 1)
	require.Len(t, f.campaignsForBook(t, withoutHP.ID), 1)
	require.Len(t, f.notifier.snapshot(), 2)
}

// DecodeFollowups / EncodeFollowups の往復 (DB 不要)。
func TestFollowupsJSONRoundTrip(t *testing.T) {
	in := []DraftFollowup{
		{WaitDays: 3, Subject: "Re: 提案", Body: "本文1"},
		{WaitDays: 7, Subject: "", Body: "本文2"},
	}
	raw, err := EncodeFollowups(in)
	require.NoError(t, err)
	require.Equal(t, in, DecodeFollowups(raw))

	// 壊れた JSON は「フォローアップ無し」に倒す (下書き生成は止めない)。
	require.Nil(t, DecodeFollowups([]byte("{not json")))
	require.Nil(t, DecodeFollowups(nil))

	// 空配列も安全。
	empty, err := EncodeFollowups(nil)
	require.NoError(t, err)
	require.Equal(t, json.RawMessage("[]"), json.RawMessage(empty))
	require.Empty(t, DecodeFollowups(empty))
}
