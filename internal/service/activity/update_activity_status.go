package activity

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// UpdateActivityStatus は既存 Activity の status_id だけを更新する。
// 認可: Activity が属する Customer の Book に editor 以上必須。
func (s *ActivityService) UpdateActivityStatus(
	ctx context.Context,
	req *connect.Request[activityv1.UpdateActivityStatusRequest],
) (*connect.Response[activityv1.UpdateActivityStatusResponse], error) {
	activityID, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid activity id: %w", err))
	}
	statusID, err := uuid.Parse(req.Msg.StatusId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid status_id: %w", err))
	}

	existing, err := s.queries.GetActivity(ctx, activityID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("activity not found: %w", err))
	}
	customer, err := s.queries.GetCustomer(ctx, existing.CustomerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customer lookup: %w", err))
	}
	if err := s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleEditor); err != nil {
		return nil, err
	}

	updated, err := s.queries.UpdateActivityStatus(ctx, db.UpdateActivityStatusParams{
		ID:       activityID,
		StatusID: pgtype.UUID{Bytes: statusID, Valid: true},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update activity: %w", err))
	}

	return connect.NewResponse(&activityv1.UpdateActivityStatusResponse{
		Activity: modelToProto(updated),
	}), nil
}
