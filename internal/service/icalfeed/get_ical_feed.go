package icalfeed

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	icalfeedv1 "github.com/0utl1er-tech/phox-customer/gen/pb/icalfeed/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/jackc/pgx/v5"
)

// GetICalFeed は現在の feed 情報を返す。未生成なら feed フィールドが nil。
func (s *ICalFeedService) GetICalFeed(
	ctx context.Context,
	req *connect.Request[icalfeedv1.GetICalFeedRequest],
) (*connect.Response[icalfeedv1.GetICalFeedResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	row, err := s.queries.GetUserICalFeed(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return connect.NewResponse(&icalfeedv1.GetICalFeedResponse{}), nil
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get feed: %w", err))
	}

	info := modelToProto(row, s.icalFeedBaseURL)
	return connect.NewResponse(&icalfeedv1.GetICalFeedResponse{
		Feed: info,
	}), nil
}
