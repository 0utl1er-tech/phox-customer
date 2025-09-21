package customer

import (
	"context"

	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	staffv1 "github.com/0utl1er-tech/phox-customer/gen/pb/staff/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
)

// CreateCustomer 新しいcustomerを作成
func (s *CustomerService) CreateCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.CreateCustomerRequest],
) (*connect.Response[customerv1.CreateCustomerResponse], error) {

	customerID := uuid.New()
	picID := uuid.New()
	leaderID := uuid.New()

	pic, err := s.queries.CreateStaff(ctx, db.CreateStaffParams{
		ID:   picID,
		Name: req.Msg.Pic.Name,
		Sex:  req.Msg.Pic.Sex,
	})

	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	leader, err := s.queries.CreateStaff(ctx, db.CreateStaffParams{
		ID:   leaderID,
		Name: req.Msg.Leader.Name,
		Sex:  req.Msg.Leader.Sex,
	})

	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	customer, err := s.queries.CreateCustomer(ctx, db.CreateCustomerParams{
		ID:          customerID,
		BookID:      uuid.MustParse(req.Msg.BookId),
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
			Id:     customer.ID.String(),
			BookId: customer.BookID.String(),
			Name:   customer.Name,
			Pic: &staffv1.Staff{
				Id:   pic.ID.String(),
				Name: pic.Name,
				Sex:  pic.Sex,
			},
			Leader: &staffv1.Staff{
				Id:   leader.ID.String(),
				Name: leader.Name,
				Sex:  leader.Sex,
			},
		},
	}), nil
}
