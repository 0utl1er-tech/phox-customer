package customer

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/search"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// CreateCustomer 新しいcustomerを作成
//
// 注意: 以前はこの RPC に permit チェックが無く、任意のユーザーが任意の Book に
// Customer を追加できる状態だった。DeleteCustomer と同じパターンで owner/editor
// 権限を要求するように修正済み。
func (s *CustomerService) CreateCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.CreateCustomerRequest],
) (*connect.Response[customerv1.CreateCustomerResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	bookID, err := uuid.Parse(req.Msg.BookId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid book_id: %w", err))
	}

	permit, err := s.queries.GetPermitByBookIDAndUserID(ctx, db.GetPermitByBookIDAndUserIDParams{
		BookID: bookID,
		UserID: userID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("指定された Book への権限がありません"))
	}
	if permit.Role != db.RoleOwner && permit.Role != db.RoleEditor {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("customerの作成にはowner権限またはeditor権限が必要です"))
	}

	customerID := uuid.New()

	customer, err := s.queries.CreateCustomer(ctx, db.CreateCustomerParams{
		ID:          customerID,
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

	// Write-after-commit ES index. 失敗しても DB 成功は返す (degraded mode)。
	if idxErr := s.indexer.IndexCustomer(ctx, search.NewCustomerDoc(
		customer.ID,
		customer.BookID,
		customer.Name,
		customer.Corporation,
		customer.Address,
		customer.Memo,
		customer.Phone,
		customer.Category,
		customer.UpdatedAt,
	)); idxErr != nil {
		log.Warn().Err(idxErr).Str("customer_id", customer.ID.String()).Msg("failed to index created customer")
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
			Mail:        customer.Mail,
		},
	}), nil
}
