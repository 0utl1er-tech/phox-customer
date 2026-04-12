package redial

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	redialv1 "github.com/0utl1er-tech/phox-customer/gen/pb/redial/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *RedialService) DeleteRedial(
	ctx context.Context,
	req *connect.Request[redialv1.DeleteRedialRequest],
) (*connect.Response[emptypb.Empty], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	id, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid id: %w", err))
	}

	existing, err := s.queries.GetRedial(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("redial not found: %w", err))
	}
	customer, err := s.queries.GetCustomer(ctx, existing.CustomerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("customer not found: %w", err))
	}
	if err := s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleEditor); err != nil {
		return nil, err
	}

	// GCal delete を先に試行 (失敗しても DB 削除は続行、カレンダーには残るが手動削除で回復可能)
	if s.gcalClient != nil && existing.GcalEventID.Valid && existing.GcalEventID.String != "" {
		if gerr := s.gcalClient.DeleteEvent(ctx, userID, existing.GcalEventID.String); gerr != nil {
			log.Warn().Err(gerr).Str("redial_id", id.String()).Msg("redial: gcal delete failed")
		}
	}

	if err := s.queries.DeleteRedial(ctx, id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete redial: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}
