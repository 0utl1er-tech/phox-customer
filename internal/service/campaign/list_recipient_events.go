package campaign

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ListRecipientEvents — 受信者のイベント履歴 (Phase 27b ドリルダウン)。
// recipient → campaign → company で自社スコープを確認する。
func (s *CampaignService) ListRecipientEvents(
	ctx context.Context,
	req *connect.Request[campaignv1.ListRecipientEventsRequest],
) (*connect.Response[campaignv1.ListRecipientEventsResponse], error) {
	u, err := s.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	recipientID, err := uuid.Parse(req.Msg.RecipientId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid recipient_id: %w", err))
	}
	rec, err := s.queries.GetCampaignRecipient(ctx, recipientID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("recipient not found"))
	}
	c, err := s.queries.GetCampaign(ctx, rec.CampaignID)
	if err != nil || c.CompanyID != u.CompanyID {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("recipient not found"))
	}
	events, err := s.queries.ListCampaignEventsByRecipient(ctx, recipientID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list events: %w", err))
	}
	protos := make([]*campaignv1.CampaignEvent, 0, len(events))
	for _, e := range events {
		protos = append(protos, &campaignv1.CampaignEvent{
			Id:        e.ID.String(),
			Kind:      e.Kind,
			Url:       e.Url,
			UserAgent: e.UserAgent,
			CreatedAt: timestamppb.New(e.CreatedAt),
		})
	}
	return connect.NewResponse(&campaignv1.ListRecipientEventsResponse{Events: protos}), nil
}
