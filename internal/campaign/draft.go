package campaign

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// draft.go は「受信者スナップショットごと draft キャンペーンを作る」中核ロジック。
//
// Phase 28f までは CampaignService.CreateCampaign の中に閉じていたが、
// 自動下書き worker (autodraft_worker.go) が同じロジックを RPC を経由せず
// 呼ぶ必要が出たためここに移した。呼び出し元は 2 つ:
//
//   - internal/service/campaign.CreateCampaign … 認証ユーザー名義 (RPC)
//   - AutoDraftWorker                          … テンプレの creator_user_id 名義
//
// 権限判定を context のトークンではなく明示の user_id で行うのが RPC 版との
// 唯一の違い (worker にはトークンが無いため)。判定内容は同じ:
// Mailbox は editor 以上、受信者が属する Book も editor 以上。

// MXCheckBudget は作成時 MX 検証の全体予算。これを超えた分は「判定不能」
// として送信対象に残す (作成 API を待たせない)。未検証ドメインは送信直前に
// worker 側でも検証されるので取りこぼしにはならない。
const MXCheckBudget = 15 * time.Second

// MaxRecipients は customer_ids + book_ids 展開後の受信者スナップショット上限。
// proto の customer_ids max_items と同じ値 (book 展開分もこの上限に収める)。
const MaxRecipients = 10000

// DraftSchedule はペーシング設定 (proto の CampaignSchedule と同型)。
type DraftSchedule struct {
	SendStartHour        int32
	SendEndHour          int32
	SendDays             int32
	DailyCapPerMailbox   int32
	MinIntervalSec       int32
	WarmupEnabled        bool
	BouncePauseThreshold int32
}

// DefaultSchedule は未指定時の既定ペーシング (平日 9-18 時、100 通/日)。
func DefaultSchedule() DraftSchedule {
	return DraftSchedule{
		SendStartHour: 9, SendEndHour: 18, SendDays: 31,
		DailyCapPerMailbox: 100, MinIntervalSec: 90, WarmupEnabled: true,
		BouncePauseThreshold: 5,
	}
}

// DraftSender は特定電子メール法の送信者表示。
type DraftSender struct {
	Org     string
	Address string
	Contact string
}

// DraftFollowup は 2 通目以降の追いメール (proto の CampaignFollowup と同型)。
// step_no は配列順で自動採番される。
type DraftFollowup struct {
	WaitDays int32
	Subject  string
	Body     string
}

// DraftInput は draft キャンペーン 1 本分の作成パラメータ。
type DraftInput struct {
	CompanyID     uuid.UUID
	CreatorUserID string // 名義。この User の権限で Book/Mailbox を検証する

	Name        string
	Subject     string
	Body        string
	TrackOpens  bool
	TrackClicks bool

	MailboxIDs  []uuid.UUID
	CustomerIDs []uuid.UUID
	BookIDs     []uuid.UUID // Book 内全顧客をサーバ側で展開 (Phase 28a)

	Schedule  *DraftSchedule // nil なら DefaultSchedule()
	Sender    DraftSender
	Followups []DraftFollowup

	// Phase 28f: 自動下書きの由来 Book。UNIQUE 制約により同じ Book からは
	// 1 本しか作れない (二重作成の防止)。手動作成では nil。
	SourceBookID *uuid.UUID
}

// DraftResult は作成結果 + スナップショットの内訳。
type DraftResult struct {
	Campaign          db.Campaign
	Queued            int32
	SkippedNoEmail    int32
	SkippedSuppressed int32
	SkippedDuplicate  int32
	SkippedNoMX       int32
	RoleAddressCount  int32
}

// DraftCreator は draft 作成ロジックの実体。
type DraftCreator struct {
	queries *db.Queries
	dbPool  *pgxpool.Pool
}

func NewDraftCreator(queries *db.Queries, dbPool *pgxpool.Pool) *DraftCreator {
	return &DraftCreator{queries: queries, dbPool: dbPool}
}

// Create は受信者スナップショットごと draft キャンペーンを作る。
//
//   - 受信者は customer_ids と book_ids の union のスナップショット
//     (保存フィルタではない)。実行中にリストが動的に変わらないこと・
//     監査可能なことを優先する。
//   - メール無し / 会社サプレッション済み / キャンペーン内アドレス重複 /
//     MX 無しは skipped 行として記録し、内訳を返す。
//
// 返るエラーは connect.Error (呼び出し元の RPC がそのまま返せる形)。
func (c *DraftCreator) Create(ctx context.Context, in DraftInput) (DraftResult, error) {
	var zero DraftResult

	// ── Mailbox プールの検証 (editor 権限 + 同一会社 + active) ──
	mailboxIDs := make([]uuid.UUID, 0, len(in.MailboxIDs))
	seenMb := map[uuid.UUID]bool{}
	for _, mbID := range in.MailboxIDs {
		if seenMb[mbID] {
			continue
		}
		seenMb[mbID] = true
		if err := c.CheckMailboxEditor(ctx, in.CreatorUserID, mbID); err != nil {
			return zero, err
		}
		mb, gerr := c.queries.GetMailbox(ctx, mbID)
		if gerr != nil || mb.CompanyID != in.CompanyID {
			return zero, connect.NewError(connect.CodeNotFound, errors.New("mailbox not found"))
		}
		if !mb.Active {
			return zero, connect.NewError(connect.CodeFailedPrecondition,
				fmt.Errorf("メールボックス %s は無効化されています", mb.Address))
		}
		mailboxIDs = append(mailboxIDs, mbID)
	}
	if len(mailboxIDs) == 0 {
		return zero, connect.NewError(connect.CodeInvalidArgument, errors.New("mailbox_ids is required"))
	}

	// ── 受信者候補の取得 + Book 権限チェック ──
	customerIDs := make([]uuid.UUID, 0, len(in.CustomerIDs))
	seenCustomer := map[uuid.UUID]bool{}
	for _, cid := range in.CustomerIDs {
		if !seenCustomer[cid] {
			seenCustomer[cid] = true
			customerIDs = append(customerIDs, cid)
		}
	}

	// book_ids は「Book 内全顧客」の指定。権限チェックは顧客展開より先に
	// Book 単位で直接行う (空の Book でも permission denied が正しく出る)。
	checkedBooks := map[uuid.UUID]bool{}
	bookIDs := make([]uuid.UUID, 0, len(in.BookIDs))
	for _, bID := range in.BookIDs {
		if checkedBooks[bID] {
			continue
		}
		if err := c.CheckBookEditor(ctx, in.CreatorUserID, bID); err != nil {
			return zero, err
		}
		checkedBooks[bID] = true
		bookIDs = append(bookIDs, bID)
	}
	if len(customerIDs) == 0 && len(bookIDs) == 0 {
		return zero, connect.NewError(connect.CodeInvalidArgument,
			errors.New("customer_ids または book_ids のいずれかを指定してください"))
	}

	customers, err := c.queries.GetCustomersByIDs(ctx, customerIDs)
	if err != nil {
		return zero, connect.NewError(connect.CodeInternal, fmt.Errorf("fetch customers: %w", err))
	}
	if len(customers) != len(customerIDs) {
		return zero, connect.NewError(connect.CodeNotFound,
			fmt.Errorf("%d 件の顧客が見つかりません", len(customerIDs)-len(customers)))
	}
	// book_ids の展開 → customer_ids 分と union (customer_id で dedup)。
	if len(bookIDs) > 0 {
		bookCustomers, berr := c.queries.ListAllCustomersByBook(ctx, bookIDs)
		if berr != nil {
			return zero, connect.NewError(connect.CodeInternal, fmt.Errorf("expand book_ids: %w", berr))
		}
		for _, bc := range bookCustomers {
			if seenCustomer[bc.ID] {
				continue
			}
			seenCustomer[bc.ID] = true
			customers = append(customers, db.GetCustomersByIDsRow(bc))
		}
	}
	if len(customers) > MaxRecipients {
		return zero, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("受信者が上限 %d 件を超えています (book_ids 展開後 %d 件)", MaxRecipients, len(customers)))
	}
	for _, cu := range customers {
		if checkedBooks[cu.BookID] {
			continue
		}
		if err := c.CheckBookEditor(ctx, in.CreatorUserID, cu.BookID); err != nil {
			return zero, err
		}
		checkedBooks[cu.BookID] = true
	}

	// ── サプレッション一括チェック ──
	emails := make([]string, 0, len(customers))
	for _, cu := range customers {
		if e := NormalizeEmail(cu.Mail); e != "" {
			emails = append(emails, e)
		}
	}
	suppressedList, err := c.queries.ListSuppressedEmailsIn(ctx, db.ListSuppressedEmailsInParams{
		CompanyID: in.CompanyID,
		Emails:    emails,
	})
	if err != nil {
		return zero, connect.NewError(connect.CodeInternal, fmt.Errorf("check suppressions: %w", err))
	}
	suppressed := map[string]bool{}
	for _, e := range suppressedList {
		suppressed[e] = true
	}

	// ── Phase 27f: 宛先ドメインの MX 検証 ──
	// MX が無いドメインは送ればほぼ確実にハードバウンスになる。送る前に落として
	// バウンス率 (= 送信ドメインの評価) を汚さない。DNS 障害時は判定不能として
	// 送信対象に残す (安全側)。全体にタイムアウトを掛け、作成 API を待たせない。
	mxCtx, mxCancel := context.WithTimeout(ctx, MXCheckBudget)
	defer mxCancel()
	uniqueDomains := map[string]bool{}
	for _, e := range emails {
		if d := DomainOf(e); d != "" {
			uniqueDomains[d] = true
		}
	}
	domainList := make([]string, 0, len(uniqueDomains))
	for d := range uniqueDomains {
		domainList = append(domainList, d)
	}
	mxResults := NewMXChecker(c.queries).CheckMany(mxCtx, domainList)

	// ── 受信者行の組み立て (queued / skipped 分類) ──
	campaignID := uuid.New()
	rows := make([]db.CreateCampaignRecipientsParams, 0, len(customers))
	seenEmail := map[string]bool{}
	res := DraftResult{}
	for _, cu := range customers {
		email := NormalizeEmail(cu.Mail)
		row := db.CreateCampaignRecipientsParams{
			ID:         uuid.New(),
			CampaignID: campaignID,
			CustomerID: cu.ID,
			Email:      email,
			Status:     "queued",
		}
		mx, mxKnown := mxResults[DomainOf(email)]
		switch {
		case email == "":
			row.Status, row.Error = "skipped", "メールアドレス未登録"
			res.SkippedNoEmail++
		case suppressed[email]:
			row.Status, row.Error = "skipped", "配信停止済み (サプレッションリスト)"
			res.SkippedSuppressed++
		case seenEmail[email]:
			row.Status, row.Error = "skipped", "キャンペーン内でアドレス重複"
			res.SkippedDuplicate++
		case mxKnown && !mx.HasMX && !mx.Unknown:
			row.Status, row.Error = "skipped", "配信不能ドメイン (MX レコードなし)"
			res.SkippedNoMX++
		default:
			seenEmail[email] = true
			res.Queued++
			// 除外はしないが、苦情率が上がりやすいので件数だけ返す。
			if IsRoleAddress(email) {
				res.RoleAddressCount++
			}
		}
		rows = append(rows, row)
	}
	if res.Queued == 0 {
		return zero, connect.NewError(connect.CodeInvalidArgument,
			errors.New("送信可能な受信者が 0 件です (メールアドレス未登録・配信停止済み・配信不能ドメインを除外した結果)"))
	}

	// ── スケジュール/送信者情報 (未指定はデフォルト) ──
	sched := DefaultSchedule()
	if in.Schedule != nil {
		sched = *in.Schedule
	}
	if sched.SendEndHour <= sched.SendStartHour {
		return zero, connect.NewError(connect.CodeInvalidArgument, errors.New("send_end_hour must be after send_start_hour"))
	}

	sourceBook := pgtype.UUID{}
	if in.SourceBookID != nil {
		sourceBook = pgtype.UUID{Bytes: *in.SourceBookID, Valid: true}
	}

	// ── トランザクションで Campaign + プール + 受信者を書く ──
	tx, err := c.dbPool.Begin(ctx)
	if err != nil {
		return zero, connect.NewError(connect.CodeInternal, fmt.Errorf("begin tx: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := c.queries.WithTx(tx)

	created, err := q.CreateCampaign(ctx, db.CreateCampaignParams{
		ID:                   campaignID,
		CompanyID:            in.CompanyID,
		CreatedBy:            in.CreatorUserID,
		Name:                 in.Name,
		Subject:              in.Subject,
		Body:                 in.Body,
		TrackOpens:           in.TrackOpens,
		TrackClicks:          in.TrackClicks,
		SendStartHour:        sched.SendStartHour,
		SendEndHour:          sched.SendEndHour,
		SendDays:             sched.SendDays,
		DailyCapPerMailbox:   sched.DailyCapPerMailbox,
		MinIntervalSec:       sched.MinIntervalSec,
		WarmupEnabled:        sched.WarmupEnabled,
		BouncePauseThreshold: sched.BouncePauseThreshold,
		SenderOrg:            in.Sender.Org,
		SenderAddress:        in.Sender.Address,
		SenderContact:        in.Sender.Contact,
		SourceBookID:         sourceBook,
	})
	if err != nil {
		// source_book_id の UNIQUE 違反 = 他 pod / 前の tick が同じ Book から
		// 既に作っている。呼び出し元 (worker) が no-op に倒せるよう素の
		// エラーを包んで返す (IsDuplicateSourceBook で判定できる)。
		if isUniqueViolation(err) {
			return zero, errDuplicateSourceBook
		}
		return zero, connect.NewError(connect.CodeInternal, fmt.Errorf("create campaign: %w", err))
	}
	for _, mbID := range mailboxIDs {
		if err := q.AddCampaignMailbox(ctx, db.AddCampaignMailboxParams{
			CampaignID: campaignID, MailboxID: mbID,
		}); err != nil {
			return zero, connect.NewError(connect.CodeInternal, fmt.Errorf("add campaign mailbox: %w", err))
		}
	}
	// Phase 27e: フォローアップ (2 通目以降)。step_no は配列順で自動採番。
	for i, fu := range in.Followups {
		if err := q.CreateCampaignStep(ctx, db.CreateCampaignStepParams{
			CampaignID: campaignID,
			StepNo:     int32(i + 2),
			WaitDays:   fu.WaitDays,
			Subject:    fu.Subject,
			Body:       fu.Body,
		}); err != nil {
			return zero, connect.NewError(connect.CodeInternal, fmt.Errorf("create followup step: %w", err))
		}
	}
	if _, err := q.CreateCampaignRecipients(ctx, rows); err != nil {
		return zero, connect.NewError(connect.CodeInternal, fmt.Errorf("create recipients: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		if isUniqueViolation(err) {
			return zero, errDuplicateSourceBook
		}
		return zero, connect.NewError(connect.CodeInternal, fmt.Errorf("commit: %w", err))
	}

	res.Campaign = created
	return res, nil
}

// errDuplicateSourceBook は source_book_id の UNIQUE 違反。
var errDuplicateSourceBook = errors.New("campaign: source book already has an auto-generated draft")

// IsDuplicateSourceBook は「その Book には既に自動下書きがある」エラーか。
func IsDuplicateSourceBook(err error) bool {
	return errors.Is(err, errDuplicateSourceBook)
}

// isUniqueViolation は Postgres の 23505 (unique_violation) 判定。
// pgconn.PgError を握るために文字列判定ではなくコードで見る。
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

// CheckBookEditor は user_id が book に対し editor 以上の Permit を持つか。
// context のトークンではなく明示の user_id で判定する (worker から呼べる形)。
func (c *DraftCreator) CheckBookEditor(ctx context.Context, userID string, bookID uuid.UUID) error {
	permit, err := c.queries.GetPermitByBookIDAndUserID(ctx, db.GetPermitByBookIDAndUserIDParams{
		BookID: bookID,
		UserID: userID,
	})
	if err != nil {
		return connect.NewError(connect.CodePermissionDenied, errors.New("you do not have access to this book"))
	}
	if !roleAtLeastEditor(permit.Role) {
		return connect.NewError(connect.CodePermissionDenied, errors.New("you do not have the required permission for this action"))
	}
	return nil
}

// CheckMailboxEditor は CheckBookEditor の Mailbox 版。
func (c *DraftCreator) CheckMailboxEditor(ctx context.Context, userID string, mailboxID uuid.UUID) error {
	permit, err := c.queries.GetMailboxPermitByMailboxIDAndUserID(ctx, db.GetMailboxPermitByMailboxIDAndUserIDParams{
		MailboxID: mailboxID,
		UserID:    userID,
	})
	if err != nil {
		return connect.NewError(connect.CodePermissionDenied, errors.New("you do not have access to this mailbox"))
	}
	if !roleAtLeastEditor(permit.Role) {
		return connect.NewError(connect.CodePermissionDenied, errors.New("you do not have the required permission for this action"))
	}
	return nil
}

// roleAtLeastEditor は auth.roleSatisfies(have, RoleEditor) と同義
// (owner > editor > viewer の階層)。internal/service/auth を import すると
// 層が逆流するのでここに小さく置く。
func roleAtLeastEditor(have db.Role) bool {
	return have == db.RoleOwner || have == db.RoleEditor
}

// NormalizeEmail は小文字化 + trim。'@' を含まないものは空 (= 宛先無し) 扱い。
func NormalizeEmail(s string) string {
	e := strings.ToLower(strings.TrimSpace(s))
	if !strings.Contains(e, "@") {
		return ""
	}
	return e
}
