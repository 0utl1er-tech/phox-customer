package call

import (
	"context"

	"connectrpc.com/connect"
	callv1 "github.com/0utl1er-tech/phox-customer/gen/pb/call/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *CallService) ListCallsByCustomerID(ctx context.Context, req *connect.Request[callv1.ListCallsByCustomerIDRequest]) (*connect.Response[callv1.ListCallsByCustomerIDResponse], error) {
	customerID, err := uuid.Parse(req.Msg.CustomerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// 顧客を取得してbook_idを確認
	customer, err := s.queries.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// 権限チェック
	err = s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleViewer)
	if err != nil {
		return nil, err
	}

	calls, err := s.queries.ListCallsByCustomerID(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	pbCalls := make([]*callv1.Call, len(calls))
	for i, call := range calls {
		pbCalls[i] = &callv1.Call{
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

	return connect.NewResponse(&callv1.ListCallsByCustomerIDResponse{
		Calls: pbCalls,
	}), nil
}
