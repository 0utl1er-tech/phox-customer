package status

import (
	"context"

	"connectrpc.com/connect"
	statusv1 "github.com/0utl1er-tech/phox-customer/gen/pb/status/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
)

func (s *StatusService) DeleteStatus(ctx context.Context, req *connect.Request[statusv1.DeleteStatusRequest]) (*connect.Response[statusv1.DeleteStatusResponse], error) {
	statusID, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// ステータスを取得してbook_idを確認
	existingStatus, err := s.queries.GetStatus(ctx, statusID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// 権限チェック（editor以上が必要）
	err = s.authorizer.CheckPermission(ctx, existingStatus.BookID, db.RoleEditor)
	if err != nil {
		return nil, err
	}

	err = s.queries.DeleteStatus(ctx, statusID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&statusv1.DeleteStatusResponse{
		Id: req.Msg.Id,
	}), nil
}
