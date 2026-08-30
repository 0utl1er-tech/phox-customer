package campaign

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

// ListCampaigns — 会社のキャンペーン一覧 (サマリ stats 付き)。閲覧は会社メンバーなら可。
func (s *CampaignService) ListCampaigns(
	ctx context.Context,
	req *connect.Request[campaignv1.ListCampaignsRequest],
) (*connect.Response[campaignv1.ListCampaignsResponse], error) {
	u, err := s.currentUser(ctx)
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
	campaigns, err := s.queries.ListCampaignsByCompany(ctx, db.ListCampaignsByCompanyParams{
		CompanyID: u.CompanyID, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list campaigns: %w", err))
	}
	total, err := s.queries.CountCampaignsByCompany(ctx, u.CompanyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("count campaigns: %w", err))
	}
	protos := make([]*campaignv1.Campaign, 0, len(campaigns))
	for _, c := range campaigns {
		p, perr := s.campaignToProto(ctx, c)
		if perr != nil {
			return nil, connect.NewError(connect.CodeInternal, perr)
		}
		protos = append(protos, p)
	}
	return connect.NewResponse(&campaignv1.ListCampaignsResponse{
		Campaigns: protos,
		Total:     total,
	}), nil
}

// GetCampaign — 詳細 (stats 込み)。
func (s *CampaignService) GetCampaign(
	ctx context.Context,
	req *connect.Request[campaignv1.GetCampaignRequest],
) (*connect.Response[campaignv1.GetCampaignResponse], error) {
	c, _, err := s.getCampaignScoped(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	p, err := s.campaignToProto(ctx, c)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&campaignv1.GetCampaignResponse{Campaign: p}), nil
}
