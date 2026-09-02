package campaign

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// UpdateCampaign — draft / paused のみ編集可 (running 中の本文差し替えは
// 受信者間で内容が割れるため不可)。mailbox_ids を渡すとプールを差し替える。
func (s *CampaignService) UpdateCampaign(
	ctx context.Context,
	req *connect.Request[campaignv1.UpdateCampaignRequest],
) (*connect.Response[campaignv1.UpdateCampaignResponse], error) {
	c, u, err := s.getCampaignScoped(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if err := s.checkPoolPermission(ctx, c.ID); err != nil {
		return nil, err
	}
	if c.Status != "draft" && c.Status != "paused" {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("status %s のキャンペーンは編集できません", c.Status))
	}

	params := db.UpdateCampaignDraftParams{ID: c.ID}
	if req.Msg.Name != nil {
		params.Name = pgtype.Text{String: *req.Msg.Name, Valid: true}
	}
	if req.Msg.Subject != nil {
		params.Subject = pgtype.Text{String: *req.Msg.Subject, Valid: true}
	}
	if req.Msg.Body != nil {
		params.Body = pgtype.Text{String: *req.Msg.Body, Valid: true}
	}
	if req.Msg.TrackOpens != nil {
		params.TrackOpens = pgtype.Bool{Bool: *req.Msg.TrackOpens, Valid: true}
	}
	if req.Msg.TrackClicks != nil {
		params.TrackClicks = pgtype.Bool{Bool: *req.Msg.TrackClicks, Valid: true}
	}
	if sc := req.Msg.Schedule; sc != nil {
		if sc.SendEndHour <= sc.SendStartHour {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("send_end_hour must be after send_start_hour"))
		}
		params.SendStartHour = pgtype.Int4{Int32: sc.SendStartHour, Valid: true}
		params.SendEndHour = pgtype.Int4{Int32: sc.SendEndHour, Valid: true}
		params.SendDays = pgtype.Int4{Int32: sc.SendDays, Valid: true}
		params.DailyCapPerMailbox = pgtype.Int4{Int32: sc.DailyCapPerMailbox, Valid: true}
		params.MinIntervalSec = pgtype.Int4{Int32: sc.MinIntervalSec, Valid: true}
		params.WarmupEnabled = pgtype.Bool{Bool: sc.WarmupEnabled, Valid: true}
		params.BouncePauseThreshold = pgtype.Int4{Int32: sc.BouncePauseThreshold, Valid: true}
	}
	if sd := req.Msg.Sender; sd != nil {
		params.SenderOrg = pgtype.Text{String: sd.SenderOrg, Valid: true}
		params.SenderAddress = pgtype.Text{String: sd.SenderAddress, Valid: true}
		params.SenderContact = pgtype.Text{String: sd.SenderContact, Valid: true}
	}

	// プール差し替え (指定時のみ)。新プールにも editor 権限が必要。
	if len(req.Msg.MailboxIds) > 0 {
		newIDs := make([]uuid.UUID, 0, len(req.Msg.MailboxIds))
		seen := map[uuid.UUID]bool{}
		for _, raw := range req.Msg.MailboxIds {
			mbID, perr := uuid.Parse(raw)
			if perr != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid mailbox_id: %w", perr))
			}
			if seen[mbID] {
				continue
			}
			seen[mbID] = true
			if err := s.authorizer.CheckMailboxPermission(ctx, mbID, db.RoleEditor); err != nil {
				return nil, err
			}
			mb, gerr := s.queries.GetMailbox(ctx, mbID)
			if gerr != nil || mb.CompanyID != u.CompanyID {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("mailbox not found"))
			}
			newIDs = append(newIDs, mbID)
		}
		if err := s.queries.DeleteCampaignMailboxes(ctx, c.ID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("replace mailboxes: %w", err))
		}
		for _, mbID := range newIDs {
			if err := s.queries.AddCampaignMailbox(ctx, db.AddCampaignMailboxParams{
				CampaignID: c.ID, MailboxID: mbID,
			}); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("replace mailboxes: %w", err))
			}
		}
	}

	// Phase 27e: フォローアップの全置換 (指定時のみ)。
	if len(req.Msg.Followups) > 0 {
		if err := s.queries.DeleteCampaignSteps(ctx, c.ID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("replace followups: %w", err))
		}
		for i, fu := range req.Msg.Followups {
			if err := s.queries.CreateCampaignStep(ctx, db.CreateCampaignStepParams{
				CampaignID: c.ID,
				StepNo:     int32(i + 2),
				WaitDays:   fu.WaitDays,
				Subject:    fu.Subject,
				Body:       fu.Body,
			}); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("replace followups: %w", err))
			}
		}
	}

	updated, err := s.queries.UpdateCampaignDraft(ctx, params)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update campaign: %w", err))
	}
	p, err := s.campaignToProto(ctx, updated)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&campaignv1.UpdateCampaignResponse{Campaign: p}), nil
}
