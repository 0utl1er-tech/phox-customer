package activity

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// CreateActivityCall は電話をかけた事実を Activity として記録する。
// 認可: 対応する Book に editor 以上の権限が必要。
func (s *ActivityService) CreateActivityCall(
	ctx context.Context,
	req *connect.Request[activityv1.CreateActivityCallRequest],
) (*connect.Response[activityv1.CreateActivityCallResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	customerID, err := uuid.Parse(req.Msg.CustomerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid customer_id: %w", err))
	}
	statusID, err := uuid.Parse(req.Msg.StatusId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid status_id: %w", err))
	}

	// Customer → Book で editor 権限チェック
	customer, err := s.queries.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("customer not found: %w", err))
	}
	if err := s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleEditor); err != nil {
		return nil, err
	}

	// optional contact_id
	contactID := pgtype.UUID{Valid: false}
	if req.Msg.ContactId != nil && *req.Msg.ContactId != "" {
		cid, err := uuid.Parse(*req.Msg.ContactId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid contact_id: %w", err))
		}
		contactID = pgtype.UUID{Bytes: cid, Valid: true}
	}

	// occurred_at は手動追加時に過去日時を指定できる。未指定なら現在時刻。
	occurredAt := time.Now()
	if req.Msg.OccurredAt != nil {
		occurredAt = req.Msg.OccurredAt.AsTime()
	}

	act, err := s.queries.CreateActivity(ctx, db.CreateActivityParams{
		ID:         uuid.New(),
		CustomerID: customerID,
		ContactID:  contactID,
		Type:       "call",
		UserID:     userID,
		StatusID:   pgtype.UUID{Bytes: statusID, Valid: true},
		Phone:      pgtype.Text{String: req.Msg.Phone, Valid: true},
		OccurredAt: occurredAt,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create activity: %w", err))
	}

	return connect.NewResponse(&activityv1.CreateActivityCallResponse{
		Activity: modelToProto(act),
	}), nil
}
