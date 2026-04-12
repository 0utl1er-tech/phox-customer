package icalfeed

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	icalfeedv1 "github.com/0utl1er-tech/phox-customer/gen/pb/icalfeed/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"google.golang.org/protobuf/types/known/emptypb"
)

// RevokeICalFeed は feed 行を削除して URL を無効化する。
// 既に無ければ no-op (idempotent)。
func (s *ICalFeedService) RevokeICalFeed(
	ctx context.Context,
	req *connect.Request[icalfeedv1.RevokeICalFeedRequest],
) (*connect.Response[emptypb.Empty], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	if err := s.queries.DeleteUserICalFeed(ctx, userID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete feed: %w", err))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}
