package campaign

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	campaignpkg "github.com/0utl1er-tech/phox-customer/internal/campaign"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
)

// UpdateCampaignAutoDraft は自動下書きテンプレートを部分更新する (owner のみ)。
// 未指定フィールドは変更しない。有効/無効トグルもこの RPC で行う。
func (s *CampaignService) UpdateCampaignAutoDraft(
	ctx context.Context,
	req *connect.Request[campaignv1.UpdateCampaignAutoDraftRequest],
) (*connect.Response[campaignv1.UpdateCampaignAutoDraftResponse], error) {
	existing, _, err := s.getAutoDraftScoped(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}

	params := db.UpdateCampaignAutoDraftParams{ID: existing.ID}
	if req.Msg.Name != nil {
		params.Name = pgtype.Text{String: req.Msg.GetName(), Valid: true}
	}
	if req.Msg.BookNamePattern != nil {
		if req.Msg.GetBookNamePattern() == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("book_name_pattern を空にはできません"))
		}
		params.BookNamePattern = pgtype.Text{String: req.Msg.GetBookNamePattern(), Valid: true}
	}
	if req.Msg.Enabled != nil {
		params.Enabled = pgtype.Bool{Bool: req.Msg.GetEnabled(), Valid: true}
	}
	if req.Msg.Subject != nil {
		params.Subject = pgtype.Text{String: req.Msg.GetSubject(), Valid: true}
	}
	if req.Msg.Body != nil {
		params.Body = pgtype.Text{String: req.Msg.GetBody(), Valid: true}
	}
	if req.Msg.TrackOpens != nil {
		params.TrackOpens = pgtype.Bool{Bool: req.Msg.GetTrackOpens(), Valid: true}
	}
	if req.Msg.TrackClicks != nil {
		params.TrackClicks = pgtype.Bool{Bool: req.Msg.GetTrackClicks(), Valid: true}
	}
	// followups: 空 repeated は「変更しない」。全消しは clear_followups で。
	if len(req.Msg.Followups) > 0 || req.Msg.ClearFollowups {
		encoded, eerr := campaignpkg.EncodeFollowups(followupsFromProto(req.Msg.Followups))
		if eerr != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encode followups: %w", eerr))
		}
		params.Followups = encoded
	}
	// mailbox_ids: 空なら変更しない (プールを空にはできない)。
	if len(req.Msg.MailboxIds) > 0 {
		ids, merr := s.checkAutoDraftMailboxes(ctx, req.Msg.MailboxIds)
		if merr != nil {
			return nil, merr
		}
		params.MailboxIds = ids
	}
	if req.Msg.Schedule != nil {
		sched, serr := validateAutoDraftSchedule(req.Msg.Schedule)
		if serr != nil {
			return nil, serr
		}
		params.SendStartHour = pgtype.Int4{Int32: sched.SendStartHour, Valid: true}
		params.SendEndHour = pgtype.Int4{Int32: sched.SendEndHour, Valid: true}
		params.SendDays = pgtype.Int4{Int32: sched.SendDays, Valid: true}
		params.DailyCapPerMailbox = pgtype.Int4{Int32: sched.DailyCapPerMailbox, Valid: true}
		params.MinIntervalSec = pgtype.Int4{Int32: sched.MinIntervalSec, Valid: true}
		params.WarmupEnabled = pgtype.Bool{Bool: sched.WarmupEnabled, Valid: true}
		params.BouncePauseThreshold = pgtype.Int4{Int32: sched.BouncePauseThreshold, Valid: true}
	}
	if req.Msg.Sender != nil {
		sender := senderFromProto(req.Msg.Sender)
		params.SenderOrg = pgtype.Text{String: sender.Org, Valid: true}
		params.SenderAddress = pgtype.Text{String: sender.Address, Valid: true}
		params.SenderContact = pgtype.Text{String: sender.Contact, Valid: true}
	}

	ad, err := s.queries.UpdateCampaignAutoDraft(ctx, params)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update auto draft: %w", err))
	}
	log.Info().
		Str("auto_draft_id", ad.ID.String()).
		Str("pattern", ad.BookNamePattern).
		Bool("enabled", ad.Enabled).
		Msg("campaign auto-draft: template updated")

	return connect.NewResponse(&campaignv1.UpdateCampaignAutoDraftResponse{
		AutoDraft: autoDraftToProto(ad),
	}), nil
}
