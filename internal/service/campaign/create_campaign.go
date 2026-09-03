package campaign

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"time"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	campaignpkg "github.com/0utl1er-tech/phox-customer/internal/campaign"
	"github.com/google/uuid"
)

// mxCheckBudget は作成時 MX 検証の全体予算。これを超えた分は「判定不能」
// として送信対象に残す (作成 API を待たせない)。未検証ドメインは送信直前に
// worker 側でも検証されるので取りこぼしにはならない。
const mxCheckBudget = 15 * time.Second

// CreateCampaign は受信者スナップショットごと draft キャンペーンを作る。
//
//   - 受信者は明示的な customer_ids のスナップショット (保存フィルタではない)。
//     実行中にリストが動的に変わらないこと・監査可能なことを優先する。
//   - メール無し / 会社サプレッション済み / キャンペーン内アドレス重複は
//     skipped 行として記録し、内訳をレスポンスで返す (UI が
//     「1,240 選択 → 1,180 queued (60 skipped)」を出せるように)。
//
// RBAC: 全 mailbox_ids への editor 以上 + 受信者が属する全 Book への editor 以上。
func (s *CampaignService) CreateCampaign(
	ctx context.Context,
	req *connect.Request[campaignv1.CreateCampaignRequest],
) (*connect.Response[campaignv1.CreateCampaignResponse], error) {
	u, err := s.currentUser(ctx)
	if err != nil {
		return nil, err
	}

	// ── Mailbox プールの検証 (editor 権限 + 同一会社 + active) ──
	mailboxIDs := make([]uuid.UUID, 0, len(req.Msg.MailboxIds))
	seenMb := map[uuid.UUID]bool{}
	for _, raw := range req.Msg.MailboxIds {
		mbID, perr := uuid.Parse(raw)
		if perr != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid mailbox_id: %w", perr))
		}
		if seenMb[mbID] {
			continue
		}
		seenMb[mbID] = true
		if err := s.authorizer.CheckMailboxPermission(ctx, mbID, db.RoleEditor); err != nil {
			return nil, err
		}
		mb, gerr := s.queries.GetMailbox(ctx, mbID)
		if gerr != nil || mb.CompanyID != u.CompanyID {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("mailbox not found"))
		}
		if !mb.Active {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				fmt.Errorf("メールボックス %s は無効化されています", mb.Address))
		}
		mailboxIDs = append(mailboxIDs, mbID)
	}
	if len(mailboxIDs) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("mailbox_ids is required"))
	}

	// ── 受信者候補の取得 + Book 権限チェック ──
	customerIDs := make([]uuid.UUID, 0, len(req.Msg.CustomerIds))
	seenCustomer := map[uuid.UUID]bool{}
	for _, raw := range req.Msg.CustomerIds {
		cid, perr := uuid.Parse(raw)
		if perr != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid customer_id: %w", perr))
		}
		if !seenCustomer[cid] {
			seenCustomer[cid] = true
			customerIDs = append(customerIDs, cid)
		}
	}
	customers, err := s.queries.GetCustomersByIDs(ctx, customerIDs)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("fetch customers: %w", err))
	}
	if len(customers) != len(customerIDs) {
		return nil, connect.NewError(connect.CodeNotFound,
			fmt.Errorf("%d 件の顧客が見つかりません", len(customerIDs)-len(customers)))
	}
	checkedBooks := map[uuid.UUID]bool{}
	for _, c := range customers {
		if checkedBooks[c.BookID] {
			continue
		}
		if err := s.authorizer.CheckPermission(ctx, c.BookID, db.RoleEditor); err != nil {
			return nil, err
		}
		checkedBooks[c.BookID] = true
	}

	// ── サプレッション一括チェック ──
	emails := make([]string, 0, len(customers))
	for _, c := range customers {
		if e := normalizeEmail(c.Mail); e != "" {
			emails = append(emails, e)
		}
	}
	suppressedList, err := s.queries.ListSuppressedEmailsIn(ctx, db.ListSuppressedEmailsInParams{
		CompanyID: u.CompanyID,
		Emails:    emails,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("check suppressions: %w", err))
	}
	suppressed := map[string]bool{}
	for _, e := range suppressedList {
		suppressed[e] = true
	}

	// ── Phase 27f: 宛先ドメインの MX 検証 ──
	// MX が無いドメインは送ればほぼ確実にハードバウンスになる。送る前に落として
	// バウンス率 (= 送信ドメインの評価) を汚さない。DNS 障害時は判定不能として
	// 送信対象に残す (安全側)。全体にタイムアウトを掛け、作成 API を待たせない。
	mxCtx, mxCancel := context.WithTimeout(ctx, mxCheckBudget)
	defer mxCancel()
	uniqueDomains := map[string]bool{}
	for _, e := range emails {
		if d := campaignpkg.DomainOf(e); d != "" {
			uniqueDomains[d] = true
		}
	}
	domainList := make([]string, 0, len(uniqueDomains))
	for d := range uniqueDomains {
		domainList = append(domainList, d)
	}
	mxResults := campaignpkg.NewMXChecker(s.queries).CheckMany(mxCtx, domainList)

	// ── 受信者行の組み立て (queued / skipped 分類) ──
	campaignID := uuid.New()
	rows := make([]db.CreateCampaignRecipientsParams, 0, len(customers))
	seenEmail := map[string]bool{}
	var queued, noEmail, wasSuppressed, duplicate, noMX, roleAddr int32
	for _, c := range customers {
		email := normalizeEmail(c.Mail)
		row := db.CreateCampaignRecipientsParams{
			ID:         uuid.New(),
			CampaignID: campaignID,
			CustomerID: c.ID,
			Email:      email,
			Status:     "queued",
		}
		mx, mxKnown := mxResults[campaignpkg.DomainOf(email)]
		switch {
		case email == "":
			row.Status, row.Error = "skipped", "メールアドレス未登録"
			noEmail++
		case suppressed[email]:
			row.Status, row.Error = "skipped", "配信停止済み (サプレッションリスト)"
			wasSuppressed++
		case seenEmail[email]:
			row.Status, row.Error = "skipped", "キャンペーン内でアドレス重複"
			duplicate++
		case mxKnown && !mx.HasMX && !mx.Unknown:
			row.Status, row.Error = "skipped", "配信不能ドメイン (MX レコードなし)"
			noMX++
		default:
			seenEmail[email] = true
			queued++
			// 除外はしないが、苦情率が上がりやすいので件数だけ返す。
			if campaignpkg.IsRoleAddress(email) {
				roleAddr++
			}
		}
		rows = append(rows, row)
	}
	if queued == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("送信可能な受信者が 0 件です (メールアドレス未登録・配信停止済み・配信不能ドメインを除外した結果)"))
	}

	// ── スケジュール/送信者情報 (未指定はデフォルト) ──
	sched := req.Msg.Schedule
	if sched == nil {
		sched = &campaignv1.CampaignSchedule{
			SendStartHour: 9, SendEndHour: 18, SendDays: 31,
			DailyCapPerMailbox: 100, MinIntervalSec: 90, WarmupEnabled: true,
			BouncePauseThreshold: 5,
		}
	}
	if sched.SendEndHour <= sched.SendStartHour {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("send_end_hour must be after send_start_hour"))
	}
	sender := req.Msg.Sender
	if sender == nil {
		sender = &campaignv1.CampaignSender{}
	}

	// ── トランザクションで Campaign + プール + 受信者を書く ──
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("begin tx: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	created, err := q.CreateCampaign(ctx, db.CreateCampaignParams{
		ID:                   campaignID,
		CompanyID:            u.CompanyID,
		CreatedBy:            u.ID,
		Name:                 req.Msg.Name,
		Subject:              req.Msg.Subject,
		Body:                 req.Msg.Body,
		TrackOpens:           req.Msg.TrackOpens,
		TrackClicks:          req.Msg.TrackClicks,
		SendStartHour:        sched.SendStartHour,
		SendEndHour:          sched.SendEndHour,
		SendDays:             sched.SendDays,
		DailyCapPerMailbox:   sched.DailyCapPerMailbox,
		MinIntervalSec:       sched.MinIntervalSec,
		WarmupEnabled:        sched.WarmupEnabled,
		BouncePauseThreshold: sched.BouncePauseThreshold,
		SenderOrg:            sender.SenderOrg,
		SenderAddress:        sender.SenderAddress,
		SenderContact:        sender.SenderContact,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create campaign: %w", err))
	}
	for _, mbID := range mailboxIDs {
		if err := q.AddCampaignMailbox(ctx, db.AddCampaignMailboxParams{
			CampaignID: campaignID, MailboxID: mbID,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("add campaign mailbox: %w", err))
		}
	}
	// Phase 27e: フォローアップ (2 通目以降)。step_no は配列順で自動採番。
	for i, fu := range req.Msg.Followups {
		if err := q.CreateCampaignStep(ctx, db.CreateCampaignStepParams{
			CampaignID: campaignID,
			StepNo:     int32(i + 2),
			WaitDays:   fu.WaitDays,
			Subject:    fu.Subject,
			Body:       fu.Body,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create followup step: %w", err))
		}
	}
	if _, err := q.CreateCampaignRecipients(ctx, rows); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create recipients: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("commit: %w", err))
	}

	proto, err := s.campaignToProto(ctx, created)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&campaignv1.CreateCampaignResponse{
		Campaign:          proto,
		QueuedCount:       queued,
		SkippedNoEmail:    noEmail,
		SkippedSuppressed: wasSuppressed,
		SkippedDuplicate:  duplicate,
		SkippedNoMx:       noMX,
		RoleAddressCount:  roleAddr,
	}), nil
}

func normalizeEmail(s string) string {
	e := strings.ToLower(strings.TrimSpace(s))
	if !strings.Contains(e, "@") {
		return ""
	}
	return e
}
