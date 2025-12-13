package customer

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
)

// DeleteCustomer customerを削除
func (s *CustomerService) DeleteCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.DeleteCustomerRequest],
) (*connect.Response[customerv1.DeleteCustomerResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	customer, err := s.queries.GetCustomer(ctx, uuid.MustParse(req.Msg.CustomerId))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの取得に失敗しました: %w", err))
	}

	permit, err := s.queries.GetPermitByBookIDAndUserID(ctx, db.GetPermitByBookIDAndUserIDParams{
		BookID: customer.BookID,
		UserID: userID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの取得に失敗しました: %w", err))
	}

	if permit.Role != db.RoleOwner && permit.Role != db.RoleEditor {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("customerの削除にはowner権限またはeditor権限が必要です"))
	}

	// customerIDをUUIDに変換
	customerID, err := uuid.Parse(req.Msg.CustomerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("無効なID形式: %w", err))
	}

	err = s.queries.DeleteCustomer(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの削除に失敗しました: %w", err))
	}

	return connect.NewResponse(&customerv1.DeleteCustomerResponse{
		CustomerId: req.Msg.CustomerId,
	}), nil
}
