package campaign

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListCampaignRecipients — 受信者一覧 (status フィルタ + ページング)。
func (s *CampaignService) ListCampaignRecipients(
	ctx context.Context,
	req *connect.Request[campaignv1.ListCampaignRecipientsRequest],
) (*connect.Response[campaignv1.ListCampaignRecipientsResponse], error) {
	c, _, err := s.getCampaignScoped(ctx, req.Msg.CampaignId)
	if err != nil {
		return nil, err
	}
	limit := int32(50)
	if req.Msg.Limit != nil {
		limit = *req.Msg.Limit
	}
	offset := int32(0)
	if req.Msg.Offset != nil {
		offset = *req.Msg.Offset
	}
	statusFilter := pgtype.Text{Valid: false}
	if req.Msg.Status != nil && *req.Msg.Status != "" {
		statusFilter = pgtype.Text{String: *req.Msg.Status, Valid: true}
	}
	rows, err := s.queries.ListCampaignRecipients(ctx, db.ListCampaignRecipientsParams{
		CampaignID: c.ID,
		Status:     statusFilter,
		PageLimit:  limit,
		PageOffset: offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list recipients: %w", err))
	}
	total, err := s.queries.CountCampaignRecipients(ctx, db.CountCampaignRecipientsParams{
		CampaignID: c.ID,
		Status:     statusFilter,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("count recipients: %w", err))
	}
	protos := make([]*campaignv1.CampaignRecipient, 0, len(rows))
	for _, r := range rows {
		protos = append(protos, recipientRowToProto(r))
	}
	return connect.NewResponse(&campaignv1.ListCampaignRecipientsResponse{
		Recipients: protos,
		Total:      total,
	}), nil
}

// RequeueFailedRecipients — failed を queued に戻す明示操作。
// worker は自動再送しない設計のため、再送はここからのみ。
func (s *CampaignService) RequeueFailedRecipients(
	ctx context.Context,
	req *connect.Request[campaignv1.RequeueFailedRecipientsRequest],
) (*connect.Response[campaignv1.RequeueFailedRecipientsResponse], error) {
	c, _, err := s.getCampaignScoped(ctx, req.Msg.CampaignId)
	if err != nil {
		return nil, err
	}
	if err := s.checkPoolPermission(ctx, c.ID); err != nil {
		return nil, err
	}
	n, err := s.queries.RequeueFailedRecipients(ctx, c.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("requeue failed recipients: %w", err))
	}
	return connect.NewResponse(&campaignv1.RequeueFailedRecipientsResponse{Requeued: n}), nil
}
