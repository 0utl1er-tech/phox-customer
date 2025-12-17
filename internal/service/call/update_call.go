package call

import (
	"context"

	"connectrpc.com/connect"
	callv1 "github.com/0utl1er-tech/phox-customer/gen/pb/call/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *CallService) UpdateCall(ctx context.Context, req *connect.Request[callv1.UpdateCallRequest]) (*connect.Response[callv1.UpdateCallResponse], error) {
	callID, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	statusID, err := uuid.Parse(req.Msg.StatusId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// コール情報を取得
	existingCall, err := s.queries.GetCall(ctx, callID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// 顧客を取得してbook_idを確認
	customer, err := s.queries.GetCustomer(ctx, existingCall.CustomerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// 権限チェック（editor以上が必要）
	err = s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleEditor)
	if err != nil {
		return nil, err
	}

	// ステータスの存在確認
	status, err := s.queries.GetStatus(ctx, statusID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// ステータスが同じbookに属していることを確認
	if status.BookID != customer.BookID {
		return nil, connect.NewError(connect.CodeInvalidArgument, nil)
	}

	// コールを更新
	updatedCall, err := s.queries.UpdateCall(ctx, db.UpdateCallParams{
		ID:       callID,
		StatusID: pgtype.UUID{Bytes: statusID, Valid: true},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// ユーザー情報を取得
	user, err := s.queries.GetUser(ctx, updatedCall.UserID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&callv1.UpdateCallResponse{
		Call: &callv1.Call{
			Id:              updatedCall.ID.String(),
			CustomerId:      updatedCall.CustomerID.String(),
			Phone:           updatedCall.Phone,
			UserId:          updatedCall.UserID,
			UserName:        user.Name,
			StatusId:        updatedCall.StatusID.String(),
			StatusName:      status.Name,
			StatusPriority:  status.Priority,
			StatusEffective: status.Effective,
			StatusNg:        status.Ng,
			CreatedAt:       timestamppb.New(updatedCall.CreatedAt),
		},
	}), nil
}
