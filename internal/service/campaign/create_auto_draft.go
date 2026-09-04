package campaign

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	campaignpkg "github.com/0utl1er-tech/phox-customer/internal/campaign"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// CreateCampaignAutoDraft は自動下書きテンプレートを 1 本作る (owner のみ)。
//
// creator_user_id は作成者本人。worker はこの名義で Book/Mailbox の権限を
// 確認して下書きを作るため、テンプレ登録時点で mailbox への editor 権限を
// 検証しておく (Book 権限は投函先が未来の Book なので tick 時に判定)。
//
// enabled は明示指定 (UI の既定は false)。有効なテンプレが 1 本も無ければ
// worker は何もしない。
func (s *CampaignService) CreateCampaignAutoDraft(
	ctx context.Context,
	req *connect.Request[campaignv1.CreateCampaignAutoDraftRequest],
) (*connect.Response[campaignv1.CreateCampaignAutoDraftResponse], error) {
	u, err := s.requireOwner(ctx)
	if err != nil {
		return nil, err
	}

	mailboxIDs, err := s.checkAutoDraftMailboxes(ctx, req.Msg.MailboxIds)
	if err != nil {
		return nil, err
	}
	sched, err := validateAutoDraftSchedule(req.Msg.Schedule)
	if err != nil {
		return nil, err
	}
	followups, err := campaignpkg.EncodeFollowups(followupsFromProto(req.Msg.Followups))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encode followups: %w", err))
	}
	sender := senderFromProto(req.Msg.Sender)

	ad, err := s.queries.CreateCampaignAutoDraft(ctx, db.CreateCampaignAutoDraftParams{
		ID:                   uuid.New(),
		CompanyID:            u.CompanyID,
		Name:                 req.Msg.Name,
		Enabled:              req.Msg.Enabled,
		BookNamePattern:      req.Msg.BookNamePattern,
		Subject:              req.Msg.Subject,
		Body:                 req.Msg.Body,
		Followups:            followups,
		MailboxIds:           mailboxIDs,
		TrackOpens:           req.Msg.TrackOpens,
		TrackClicks:          req.Msg.TrackClicks,
		SendStartHour:        sched.SendStartHour,
		SendEndHour:          sched.SendEndHour,
		SendDays:             sched.SendDays,
		DailyCapPerMailbox:   sched.DailyCapPerMailbox,
		MinIntervalSec:       sched.MinIntervalSec,
		WarmupEnabled:        sched.WarmupEnabled,
		BouncePauseThreshold: sched.BouncePauseThreshold,
		SenderOrg:            sender.Org,
		SenderAddress:        sender.Address,
		SenderContact:        sender.Contact,
		CreatorUserID:        u.ID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create auto draft: %w", err))
	}
	log.Info().
		Str("auto_draft_id", ad.ID.String()).
		Str("pattern", ad.BookNamePattern).
		Bool("enabled", ad.Enabled).
		Str("created_by", u.ID).
		Msg("campaign auto-draft: template created")

	return connect.NewResponse(&campaignv1.CreateCampaignAutoDraftResponse{
		AutoDraft: autoDraftToProto(ad),
	}), nil
}
