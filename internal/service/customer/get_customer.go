package customer

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
)

// GetCustomer returns a single customer by ID. Activity history (call / email)
// is fetched separately by the frontend via ActivityService, so this handler
// no longer embeds a `calls` list in the response.
func (s *CustomerService) GetCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.GetCustomerRequest],
) (*connect.Response[customerv1.GetCustomerResponse], error) {
	id, err := util.ParseUUID("id", req.Msg.Id)
	if err != nil {
		return nil, err
	}

	customer, err := s.queries.GetCustomer(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの取得に失敗しました: %w", err))
	}

	if err := s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleViewer); err != nil {
		return nil, err
	}

	return connect.NewResponse(&customerv1.GetCustomerResponse{
		Customer: getRowToProto(customer),
	}), nil
}
