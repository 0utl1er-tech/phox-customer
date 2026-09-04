package campaign

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	campaignpkg "github.com/0utl1er-tech/phox-customer/internal/campaign"
	"github.com/google/uuid"
)

// CreateCampaign は受信者スナップショットごと draft キャンペーンを作る。
//
// 実体は internal/campaign.DraftCreator (Phase 28f で自動下書き worker と
// 共有するため移設)。この RPC は「認証ユーザーを名義に据えて proto を
// DraftInput に詰め替える」薄い層に徹する。
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

	mailboxIDs, err := parseUUIDs(req.Msg.MailboxIds, "mailbox_id")
	if err != nil {
		return nil, err
	}
	customerIDs, err := parseUUIDs(req.Msg.CustomerIds, "customer_id")
	if err != nil {
		return nil, err
	}
	bookIDs, err := parseUUIDs(req.Msg.BookIds, "book_id")
	if err != nil {
		return nil, err
	}

	in := campaignpkg.DraftInput{
		CompanyID:     u.CompanyID,
		CreatorUserID: u.ID,
		Name:          req.Msg.Name,
		Subject:       req.Msg.Subject,
		Body:          req.Msg.Body,
		TrackOpens:    req.Msg.TrackOpens,
		TrackClicks:   req.Msg.TrackClicks,
		MailboxIDs:    mailboxIDs,
		CustomerIDs:   customerIDs,
		BookIDs:       bookIDs,
		Schedule:      scheduleFromProto(req.Msg.Schedule),
		Sender:        senderFromProto(req.Msg.Sender),
		Followups:     followupsFromProto(req.Msg.Followups),
	}

	res, err := campaignpkg.NewDraftCreator(s.queries, s.dbPool).Create(ctx, in)
	if err != nil {
		return nil, err
	}

	proto, err := s.campaignToProto(ctx, res.Campaign)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&campaignv1.CreateCampaignResponse{
		Campaign:          proto,
		QueuedCount:       res.Queued,
		SkippedNoEmail:    res.SkippedNoEmail,
		SkippedSuppressed: res.SkippedSuppressed,
		SkippedDuplicate:  res.SkippedDuplicate,
		SkippedNoMx:       res.SkippedNoMX,
		RoleAddressCount:  res.RoleAddressCount,
	}), nil
}

// parseUUIDs は文字列 ID 配列を uuid に直す (不正値は invalid_argument)。
func parseUUIDs(raws []string, field string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(raws))
	for _, raw := range raws {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid %s: %w", field, err))
		}
		out = append(out, id)
	}
	return out, nil
}

// scheduleFromProto は nil を「既定値を使う」の意味で nil のまま返す。
func scheduleFromProto(s *campaignv1.CampaignSchedule) *campaignpkg.DraftSchedule {
	if s == nil {
		return nil
	}
	return &campaignpkg.DraftSchedule{
		SendStartHour:        s.SendStartHour,
		SendEndHour:          s.SendEndHour,
		SendDays:             s.SendDays,
		DailyCapPerMailbox:   s.DailyCapPerMailbox,
		MinIntervalSec:       s.MinIntervalSec,
		WarmupEnabled:        s.WarmupEnabled,
		BouncePauseThreshold: s.BouncePauseThreshold,
	}
}

func senderFromProto(s *campaignv1.CampaignSender) campaignpkg.DraftSender {
	if s == nil {
		return campaignpkg.DraftSender{}
	}
	return campaignpkg.DraftSender{
		Org:     s.SenderOrg,
		Address: s.SenderAddress,
		Contact: s.SenderContact,
	}
}

func followupsFromProto(fus []*campaignv1.CampaignFollowup) []campaignpkg.DraftFollowup {
	out := make([]campaignpkg.DraftFollowup, 0, len(fus))
	for _, fu := range fus {
		out = append(out, campaignpkg.DraftFollowup{
			WaitDays: fu.WaitDays,
			Subject:  fu.Subject,
			Body:     fu.Body,
		})
	}
	return out
}

// normalizeEmail は suppression.go でも使う (internal/campaign と同じ実装)。
func normalizeEmail(s string) string {
	return campaignpkg.NormalizeEmail(s)
}
