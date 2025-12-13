package customer

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	callv1 "github.com/0utl1er-tech/phox-customer/gen/pb/call/v1"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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

	callRows, err := s.queries.ListCallsByCustomerID(ctx, customer.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("callsの取得に失敗しました: %w", err))
	}

	calls := make([]*callv1.Call, len(callRows))
	for i, call := range callRows {
		calls[i] = &callv1.Call{
			Id:              call.ID.String(),
			CustomerId:      call.CustomerID.String(),
			Phone:           call.Phone,
			UserId:          call.UserID,
			UserName:        call.UserName,
			StatusId:        call.StatusID.String(),
			StatusName:      call.StatusName,
			StatusPriority:  call.StatusPriority,
			StatusEffective: call.StatusEffective,
			StatusNg:        call.StatusNg,
			CreatedAt:       timestamppb.New(call.CreatedAt),
		}
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
			Calls:       calls,
		},
	}), nil
}
