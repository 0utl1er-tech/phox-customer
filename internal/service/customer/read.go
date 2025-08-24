package customer

import (
	"context"

	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
	"github.com/jackc/pgx/v5/pgtype"
)

// SearchCustomer customerを検索
func (s *ServiceImpl) SearchCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.SearchCustomerRequest],
) (*connect.Response[customerv1.SearchCustomerResponse], error) {
	// ユーザー認証とbookアクセス権限を検証
	_, bookID, err := s.validateUserAndBookAccess(ctx, req.Msg.BookId)
	if err != nil {
		return nil, err
	}

	// 検索条件を構築
	var customers []db.Customer
	if req.Msg.Name != nil && *req.Msg.Name != "" {
		// 名前で検索
		customers, err = s.queries.SearchCustomers(ctx, db.SearchCustomersParams{
			BookID: bookID,
			Name:   pgtype.Text{String: *req.Msg.Name, Valid: true},
		})
	} else {
		// 全件取得
		customers, err = s.queries.ListCustomers(ctx, bookID)
	}

	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// レスポンス用のcustomer一覧を作成
	var customerList []*customerv1.Customer
	for _, customer := range customers {
		customerList = append(customerList, convertCustomerToResponse(customer))
	}

	return connect.NewResponse(&customerv1.SearchCustomerResponse{
		Customers: customerList,
	}), nil
}
