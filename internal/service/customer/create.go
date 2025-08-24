package customer

import (
	"context"
	"fmt"

	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
)

// CreateCustomer 新しいcustomerを作成
func (s *ServiceImpl) CreateCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.CreateCustomerRequest],
) (*connect.Response[customerv1.CreateCustomerResponse], error) {
	userUUID, bookID, err := s.validateUserAndBookAccess(ctx, req.Msg.BookId)
	if err != nil {
		return nil, err
	}

	if err := s.checkUserRoleForBook(ctx, bookID, userUUID, db.RoleEditor); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("insufficient permissions: %w", err))
	}

	customerID := uuid.New()

	categoryID, err := s.createCategoryIfNeeded(ctx, req.Msg.Category, bookID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	createParams := convertCreateRequestToParams(req.Msg, customerID.String(), bookID.String(), categoryID)

	customer, err := s.queries.CreateCustomer(ctx, createParams)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&customerv1.CreateCustomerResponse{
		Id:     customer.ID.String(),
		BookId: customer.BookID.String(),
		Name:   customer.Name,
	}), nil
}
