package customer

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
)

func (s *CustomerService) ListCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.ListCustomerRequest],
) (*connect.Response[customerv1.ListCustomerResponse], error) {
	bookID, err := util.ParseUUID("book_id", req.Msg.BookId)
	if err != nil {
		return nil, err
	}

	if err := s.authorizer.CheckPermission(ctx, bookID, db.RoleViewer); err != nil {
		return nil, err
	}

	customers, err := s.queries.ListCustomers(ctx, db.ListCustomersParams{
		BookID: bookID,
		Limit:  req.Msg.Limit,
		Offset: req.Msg.Offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの取得に失敗しました: %w", err))
	}

	customerList := make([]*customerv1.Customer, 0, len(customers))
	for _, customer := range customers {
		customerList = append(customerList, listRowToProto(customer))
	}

	// ページネーション用の総件数。これが無いと UI は 51 件目以降に到達できない
	// (totalCount がページ内件数にフォールバックし「次へ」が描画されない)。
	total, err := s.queries.CountCustomersByBook(ctx, bookID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customer件数の取得に失敗しました: %w", err))
	}

	return connect.NewResponse(&customerv1.ListCustomerResponse{
		Customers:  customerList,
		TotalCount: total,
	}), nil
}
