package customer

import (
	"context"
	"fmt"

	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
)

// UpdateCustomer customerを更新
func (s *ServiceImpl) UpdateCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.UpdateCustomerRequest],
) (*connect.Response[customerv1.UpdateCustomerResponse], error) {
	// ユーザー認証とbookアクセス権限を検証
	userUUID, bookID, err := s.validateUserAndBookAccess(ctx, req.Msg.BookId)
	if err != nil {
		return nil, err
	}

	// Update操作にはeditor以上の権限が必要
	if err := s.checkUserRoleForBook(ctx, bookID, userUUID, db.RoleEditor); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("insufficient permissions: %w", err))
	}

	// TODO: 実際の更新処理を実装
	// 現在のプロトコルバッファの定義では、customer_idが指定されていないため、
	// どのcustomerを更新するかを特定する方法が必要です
	name := ""
	if req.Msg.Name != nil {
		name = *req.Msg.Name
	}
	return connect.NewResponse(&customerv1.UpdateCustomerResponse{
		Id:     "dummy-id",
		BookId: req.Msg.BookId,
		Name:   name,
	}), nil
}
