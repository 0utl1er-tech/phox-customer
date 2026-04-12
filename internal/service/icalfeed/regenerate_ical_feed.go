package icalfeed

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	icalfeedv1 "github.com/0utl1er-tech/phox-customer/gen/pb/icalfeed/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

// RegenerateICalFeed は強制的に新トークンを生成して差し替える。
// 古い URL は即座に無効化される (hard cutover)。
func (s *ICalFeedService) RegenerateICalFeed(
	ctx context.Context,
	req *connect.Request[icalfeedv1.RegenerateICalFeedRequest],
) (*connect.Response[icalfeedv1.RegenerateICalFeedResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	newTok, err := newToken()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	row, err := s.queries.UpsertUserICalFeed(ctx, db.UpsertUserICalFeedParams{
		UserID: userID,
		Token:  newTok,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("upsert feed: %w", err))
	}

	return connect.NewResponse(&icalfeedv1.RegenerateICalFeedResponse{
		Feed: modelToProto(row, s.icalFeedBaseURL),
	}), nil
}
