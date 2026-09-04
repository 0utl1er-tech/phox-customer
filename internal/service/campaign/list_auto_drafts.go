package campaign

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

// ListCampaignAutoDrafts は会社の自動下書きテンプレート一覧を返す
// (Phase 28f)。閲覧は同じ会社のユーザーなら誰でも可 — can_edit で UI 側の
// 活性を切り替える (書き込みは owner のみ)。
func (s *CampaignService) ListCampaignAutoDrafts(
	ctx context.Context,
	req *connect.Request[campaignv1.ListCampaignAutoDraftsRequest],
) (*connect.Response[campaignv1.ListCampaignAutoDraftsResponse], error) {
	u, err := s.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListCampaignAutoDraftsByCompany(ctx, u.CompanyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list auto drafts: %w", err))
	}
	out := make([]*campaignv1.CampaignAutoDraft, 0, len(rows))
	for _, ad := range rows {
		out = append(out, autoDraftToProto(ad))
	}
	return connect.NewResponse(&campaignv1.ListCampaignAutoDraftsResponse{
		AutoDrafts: out,
		CanEdit:    u.Role == db.RoleOwner,
	}), nil
}
