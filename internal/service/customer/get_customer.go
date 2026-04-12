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

// GetCustomer returns a single customer by ID. Activity history (call / email)
// is fetched separately by the frontend via ActivityService, so this handler
// no longer embeds a `calls` list in the response.
func (s *CustomerService) GetCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.GetCustomerRequest],
) (*connect.Response[customerv1.GetCustomerResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	customer, err := s.queries.GetCustomer(ctx, uuid.MustParse(req.Msg.Id))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの取得に失敗しました: %w", err))
	}

	_, err = s.queries.GetBookByIDAndUserID(ctx, db.GetBookByIDAndUserIDParams{
		ID:     customer.BookID,
		UserID: userID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("このbookにアクセスする権限がありません: %w", err))
	}

	return connect.NewResponse(&customerv1.GetCustomerResponse{
		Customer: &customerv1.Customer{
			Id:          customer.ID.String(),
			BookId:      customer.BookID.String(),
			Phone:       customer.Phone,
			Category:    customer.Category,
			Name:        customer.Name,
			Corporation: customer.Corporation,
			Address:     customer.Address,
			Memo:        customer.Memo,
			Mail:        customer.Mail,
		},
	}), nil
}
