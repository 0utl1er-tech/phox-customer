package call

import (
	"context"

	"connectrpc.com/connect"
	callv1 "github.com/0utl1er-tech/phox-customer/gen/pb/call/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *CallService) CreateCall(ctx context.Context, req *connect.Request[callv1.CreateCallRequest]) (*connect.Response[callv1.CreateCallResponse], error) {
	customerID, err := uuid.Parse(req.Msg.CustomerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	statusID, err := uuid.Parse(req.Msg.StatusId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// 顧客を取得してbook_idを確認
	customer, err := s.queries.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// 権限チェック（editor以上が必要）
	err = s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleEditor)
	if err != nil {
		return nil, err
	}

	// ユーザー情報を取得
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	// ステータスの存在確認と権限チェック
	status, err := s.queries.GetStatus(ctx, statusID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	// ステータスが同じbookに属していることを確認
	if status.BookID != customer.BookID {
		return nil, connect.NewError(connect.CodeInvalidArgument, nil)
	}

	// ユーザー情報を取得
	user, err := s.queries.GetUser(ctx, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	callID := uuid.New()
	newCall, err := s.queries.CreateCall(ctx, db.CreateCallParams{
		ID:         callID,
		CustomerID: customerID,
		Phone:      req.Msg.Phone,
		UserID:     userID,
		StatusID:   statusID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&callv1.CreateCallResponse{
		Call: &callv1.Call{
			Id:              newCall.ID.String(),
			CustomerId:      newCall.CustomerID.String(),
			Phone:           newCall.Phone,
			UserId:          newCall.UserID,
			UserName:        user.Name,
			StatusId:        newCall.StatusID.String(),
			StatusName:      status.Name,
			StatusPriority:  status.Priority,
			StatusEffective: status.Effective,
			StatusNg:        status.Ng,
			CreatedAt:       timestamppb.New(newCall.CreatedAt),
		},
	}), nil
}
