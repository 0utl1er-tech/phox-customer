package icalfeed

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	icalfeedv1 "github.com/0utl1er-tech/phox-customer/gen/pb/icalfeed/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/jackc/pgx/v5"
)

// GenerateICalFeed は既存 token があれば再利用し、無ければ新規生成して返す。
// 冪等: 既に feed を持っているユーザーが 2 回押しても同じ URL が返る。
func (s *ICalFeedService) GenerateICalFeed(
	ctx context.Context,
	req *connect.Request[icalfeedv1.GenerateICalFeedRequest],
) (*connect.Response[icalfeedv1.GenerateICalFeedResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	// 既存チェック
	if row, gerr := s.queries.GetUserICalFeed(ctx, userID); gerr == nil {
		return connect.NewResponse(&icalfeedv1.GenerateICalFeedResponse{
			Feed: modelToProto(row, s.icalFeedBaseURL),
		}), nil
	} else if !errors.Is(gerr, pgx.ErrNoRows) {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get feed: %w", gerr))
	}

	// 新規生成
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

	return connect.NewResponse(&icalfeedv1.GenerateICalFeedResponse{
		Feed: modelToProto(row, s.icalFeedBaseURL),
	}), nil
}
