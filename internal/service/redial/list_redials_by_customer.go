package redial

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	redialv1 "github.com/0utl1er-tech/phox-customer/gen/pb/redial/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
)

func (s *RedialService) ListRedialsByCustomer(
	ctx context.Context,
	req *connect.Request[redialv1.ListRedialsByCustomerRequest],
) (*connect.Response[redialv1.ListRedialsByCustomerResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	customerID, err := uuid.Parse(req.Msg.CustomerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid customer_id: %w", err))
	}

	customer, err := s.queries.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("customer not found: %w", err))
	}
	if err := s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleViewer); err != nil {
		return nil, err
	}

	rows, err := s.queries.ListRedialsByCustomer(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list redials: %w", err))
	}

	hasToken := s.lookupUserHasGoogle(ctx, userID)

	out := make([]*redialv1.Redial, 0, len(rows))
	for _, row := range rows {
		status := deriveSyncStatus(row, hasToken)
		// User 名は別途取得 (件数が少ないので 1 件ずつ)
		u, uerr := s.queries.GetUser(ctx, row.UserID)
		userName := row.UserID
		if uerr == nil {
			userName = u.Name
		}
		out = append(out, modelToProto(row, userName, status))
	}

	return connect.NewResponse(&redialv1.ListRedialsByCustomerResponse{
		Redials: out,
	}), nil
}
