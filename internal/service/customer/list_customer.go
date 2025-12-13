package customer

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
)

func (s *CustomerService) ListCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.ListCustomerRequest],
) (*connect.Response[customerv1.ListCustomerResponse], error) {
	userID := req.Header().Get("X-User-ID")
	if userID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("X-User-IDがヘッダーに見つかりません"))
	}

	_, err := s.queries.GetBookByIDAndUserID(ctx, db.GetBookByIDAndUserIDParams{
		ID:     uuid.MustParse(req.Msg.BookId),
		UserID: userID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("このbookにアクセスする権限がありません: %w", err))
	}

	customers, err := s.queries.ListCustomers(ctx, db.ListCustomersParams{
		BookID: uuid.MustParse(req.Msg.BookId),
		Limit:  req.Msg.Limit,
		Offset: req.Msg.Offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの取得に失敗しました: %w", err))
	}

	// レスポンス用のcustomer一覧を作成
	customerList := make([]*customerv1.Customer, 0, len(customers))

	for _, customer := range customers {
		customerList = append(customerList, &customerv1.Customer{
			Id:          customer.ID.String(),
			BookId:      customer.BookID.String(),
			Phone:       customer.Phone,
			Category:    customer.Category,
			Name:        customer.Name,
			Corporation: customer.Corporation,
			Address:     customer.Address,
			Memo:        customer.Memo,
		})
	}

	return connect.NewResponse(&customerv1.ListCustomerResponse{
		Customers: customerList,
	}), nil
}
