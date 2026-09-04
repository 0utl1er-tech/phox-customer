package campaign

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	campaignpkg "github.com/0utl1er-tech/phox-customer/internal/campaign"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// autodraft_helper.go は Phase 28f (キャンペーン自動下書きテンプレート) の
// 共通部品 — owner チェック・proto 変換・入力検証。

// requireOwner は呼び出しユーザーが owner であることを要求する
// (CompanyService.UpdateSettings と同じ流儀)。テンプレは「誰の名義で
// どのメールボックスから自動送信の下書きを作るか」を決める設定なので、
// 書き込みは owner に限る。
func (s *CampaignService) requireOwner(ctx context.Context) (db.User, error) {
	u, err := s.currentUser(ctx)
	if err != nil {
		return db.User{}, err
	}
	if u.Role != db.RoleOwner {
		return db.User{}, connect.NewError(connect.CodePermissionDenied,
			errors.New("自動下書きテンプレートの変更にはオーナー権限が必要です"))
	}
	return u, nil
}

// getAutoDraftScoped は id のテンプレを取得し、呼び出しユーザーと同じ会社で
// あることを確認する (owner チェック込み)。
func (s *CampaignService) getAutoDraftScoped(ctx context.Context, id string) (db.CampaignAutoDraft, db.User, error) {
	u, err := s.requireOwner(ctx)
	if err != nil {
		return db.CampaignAutoDraft{}, db.User{}, err
	}
	adID, err := uuid.Parse(id)
	if err != nil {
		return db.CampaignAutoDraft{}, db.User{}, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("invalid auto draft id: %w", err))
	}
	ad, err := s.queries.GetCampaignAutoDraft(ctx, adID)
	if err != nil || ad.CompanyID != u.CompanyID {
		return db.CampaignAutoDraft{}, db.User{}, connect.NewError(connect.CodeNotFound,
			errors.New("auto draft template not found"))
	}
	return ad, u, nil
}

// checkAutoDraftMailboxes は指定 mailbox 全てに editor 以上を要求し、
// uuid に直して返す。テンプレの mailbox は下書き作成時に呼び出しユーザー
// (= creator_user_id) の権限で使われるため、登録時点で検証しておく。
func (s *CampaignService) checkAutoDraftMailboxes(ctx context.Context, raws []string) ([]uuid.UUID, error) {
	ids, err := parseUUIDs(raws, "mailbox_id")
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(ids))
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if err := s.authorizer.CheckMailboxPermission(ctx, id, db.RoleEditor); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// validateAutoDraftSchedule は proto のペーシング設定を検証して DraftSchedule
// に直す (nil なら既定値)。
func validateAutoDraftSchedule(s *campaignv1.CampaignSchedule) (campaignpkg.DraftSchedule, error) {
	sched := campaignpkg.DefaultSchedule()
	if s != nil {
		sched = *scheduleFromProto(s)
	}
	if sched.SendEndHour <= sched.SendStartHour {
		return sched, connect.NewError(connect.CodeInvalidArgument,
			errors.New("send_end_hour must be after send_start_hour"))
	}
	return sched, nil
}

// autoDraftToProto は CampaignAutoDraft 行を proto に変換する。
func autoDraftToProto(ad db.CampaignAutoDraft) *campaignv1.CampaignAutoDraft {
	followups := make([]*campaignv1.CampaignFollowup, 0)
	for i, fu := range campaignpkg.DecodeFollowups(ad.Followups) {
		followups = append(followups, &campaignv1.CampaignFollowup{
			StepNo:   int32(i + 2),
			WaitDays: fu.WaitDays,
			Subject:  fu.Subject,
			Body:     fu.Body,
		})
	}
	mailboxIDs := make([]string, 0, len(ad.MailboxIds))
	for _, id := range ad.MailboxIds {
		mailboxIDs = append(mailboxIDs, id.String())
	}
	p := &campaignv1.CampaignAutoDraft{
		Id:              ad.ID.String(),
		Name:            ad.Name,
		Enabled:         ad.Enabled,
		BookNamePattern: ad.BookNamePattern,
		Subject:         ad.Subject,
		Body:            ad.Body,
		Followups:       followups,
		MailboxIds:      mailboxIDs,
		Schedule: &campaignv1.CampaignSchedule{
			SendStartHour:        ad.SendStartHour,
			SendEndHour:          ad.SendEndHour,
			SendDays:             ad.SendDays,
			DailyCapPerMailbox:   ad.DailyCapPerMailbox,
			MinIntervalSec:       ad.MinIntervalSec,
			WarmupEnabled:        ad.WarmupEnabled,
			BouncePauseThreshold: ad.BouncePauseThreshold,
		},
		Sender: &campaignv1.CampaignSender{
			SenderOrg:     ad.SenderOrg,
			SenderAddress: ad.SenderAddress,
			SenderContact: ad.SenderContact,
		},
		TrackOpens:    ad.TrackOpens,
		TrackClicks:   ad.TrackClicks,
		CreatorUserId: ad.CreatorUserID,
		CreatedAt:     timestamppb.New(ad.CreatedAt),
		UpdatedAt:     timestamppb.New(ad.UpdatedAt),
	}
	if ad.LastCreatedAt.Valid {
		p.LastCreatedAt = timestamppb.New(ad.LastCreatedAt.Time)
	}
	return p
}
