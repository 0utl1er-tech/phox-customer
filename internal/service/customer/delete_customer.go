package customer

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/rs/zerolog/log"
)

// DeleteCustomer customerを削除
func (s *CustomerService) DeleteCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.DeleteCustomerRequest],
) (*connect.Response[customerv1.DeleteCustomerResponse], error) {
	customerID, err := util.ParseUUID("customer_id", req.Msg.CustomerId)
	if err != nil {
		return nil, err
	}

	customer, err := s.queries.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの取得に失敗しました: %w", err))
	}

	if err := s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleEditor); err != nil {
		return nil, err
	}

	if err := s.queries.DeleteCustomer(ctx, customerID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの削除に失敗しました: %w", err))
	}

	// Write-after-commit ES unindex。失敗しても DB 成功は返す (degraded mode)。
	if idxErr := s.indexer.DeleteFromIndex(ctx, customerID.String()); idxErr != nil {
		log.Warn().Err(idxErr).Str("customer_id", customerID.String()).Msg("failed to delete customer from index")
	}

	return connect.NewResponse(&customerv1.DeleteCustomerResponse{
		CustomerId: req.Msg.CustomerId,
	}), nil
}
