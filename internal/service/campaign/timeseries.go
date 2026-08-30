package campaign

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
)

// GetCampaignTimeseries — 日次時系列 (折れ線グラフ用)。JST 日単位。
func (s *CampaignService) GetCampaignTimeseries(
	ctx context.Context,
	req *connect.Request[campaignv1.GetCampaignTimeseriesRequest],
) (*connect.Response[campaignv1.GetCampaignTimeseriesResponse], error) {
	c, _, err := s.getCampaignScoped(ctx, req.Msg.CampaignId)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.GetCampaignDailyStats(ctx, c.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("daily stats: %w", err))
	}
	days := make([]*campaignv1.CampaignDailyStat, 0, len(rows))
	for _, r := range rows {
		if !r.Day.Valid {
			continue
		}
		days = append(days, &campaignv1.CampaignDailyStat{
			Date:         r.Day.Time.Format("2006-01-02"),
			Sent:         int32(r.Sent),
			Opened:       int32(r.Opened),
			Clicked:      int32(r.Clicked),
			Replied:      int32(r.Replied),
			Bounced:      int32(r.Bounced),
			Unsubscribed: int32(r.Unsubscribed),
		})
	}
	return connect.NewResponse(&campaignv1.GetCampaignTimeseriesResponse{Days: days}), nil
}
