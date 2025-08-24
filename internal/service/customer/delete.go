package customer

import (
	"context"
	"fmt"

	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
)

// DeleteCustomer customerを削除
func (s *ServiceImpl) DeleteCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.DeleteCustomerRequest],
) (*connect.Response[customerv1.DeleteCustomerResponse], error) {
	// customerIDをUUIDに変換
	customerID, err := uuid.Parse(req.Msg.CustomerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("無効なID形式: %w", err))
	}

	customer, err := s.queries.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("Customerが見つかりません: %w", err))
	}

	userUUID, bookID, err := s.validateUserAndBookAccess(ctx, customer.BookID.String())
	if err != nil {
		return nil, err
	}

	if err := s.checkUserRoleForBook(ctx, bookID, userUUID, db.RoleEditor); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("権限が足りません: %w", err))
	}

	err = s.queries.DeleteCustomer(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("Customerの削除に失敗しました: %w", err))
	}

	return connect.NewResponse(&customerv1.DeleteCustomerResponse{
		CustomerId: req.Msg.CustomerId,
	}), nil
}
