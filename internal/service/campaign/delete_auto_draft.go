package campaign

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	"github.com/rs/zerolog/log"
)

// DeleteCampaignAutoDraft はテンプレートを削除する (owner のみ)。
// 既に生成済みの下書きキャンペーンには影響しない。
func (s *CampaignService) DeleteCampaignAutoDraft(
	ctx context.Context,
	req *connect.Request[campaignv1.DeleteCampaignAutoDraftRequest],
) (*connect.Response[campaignv1.DeleteCampaignAutoDraftResponse], error) {
	ad, _, err := s.getAutoDraftScoped(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if err := s.queries.DeleteCampaignAutoDraft(ctx, ad.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete auto draft: %w", err))
	}
	log.Info().Str("auto_draft_id", ad.ID.String()).Msg("campaign auto-draft: template deleted")
	return connect.NewResponse(&campaignv1.DeleteCampaignAutoDraftResponse{Id: ad.ID.String()}), nil
}
