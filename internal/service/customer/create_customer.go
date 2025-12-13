package customer

import (
	"context"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
)

// CreateCustomer 新しいcustomerを作成
func (s *CustomerService) CreateCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.CreateCustomerRequest],
) (*connect.Response[customerv1.CreateCustomerResponse], error) {

	customerID := uuid.New()

	customer, err := s.queries.CreateCustomer(ctx, db.CreateCustomerParams{
		ID:          customerID,
		BookID:      uuid.MustParse(req.Msg.BookId),
		Phone:       req.Msg.Phone,
		Category:    req.Msg.Category,
		Name:        req.Msg.Name,
		Corporation: req.Msg.Corporation,
		Address:     req.Msg.Address,
		Memo:        req.Msg.Memo,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&customerv1.CreateCustomerResponse{
		Customer: &customerv1.Customer{
			Id:          customer.ID.String(),
			BookId:      customer.BookID.String(),
			Phone:       customer.Phone,
			Category:    customer.Category,
			Name:        customer.Name,
			Corporation: customer.Corporation,
			Address:     customer.Address,
			Memo:        customer.Memo,
		},
	}), nil
}
