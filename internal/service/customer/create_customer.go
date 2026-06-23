package customer

import (
	"context"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/google/uuid"
)

// CreateCustomer 新しいcustomerを作成
//
// 注意: 以前はこの RPC に permit チェックが無く、任意のユーザーが任意の Book に
// Customer を追加できる状態だった。owner/editor 権限を要求するように修正済み。
func (s *CustomerService) CreateCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.CreateCustomerRequest],
) (*connect.Response[customerv1.CreateCustomerResponse], error) {
	bookID, err := util.ParseUUID("book_id", req.Msg.BookId)
	if err != nil {
		return nil, err
	}

	if err := s.authorizer.CheckPermission(ctx, bookID, db.RoleEditor); err != nil {
		return nil, err
	}

	customer, err := s.queries.CreateCustomer(ctx, db.CreateCustomerParams{
		ID:          uuid.New(),
		BookID:      bookID,
		Phone:       req.Msg.Phone,
		Category:    req.Msg.Category,
		Name:        req.Msg.Name,
		Corporation: req.Msg.Corporation,
		Address:     req.Msg.Address,
		Memo:        req.Msg.Memo,
		Mail:        req.Msg.Mail,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.indexCustomer(ctx, customer, "created")

	return connect.NewResponse(&customerv1.CreateCustomerResponse{
		Customer: modelToProto(customer),
	}), nil
}
