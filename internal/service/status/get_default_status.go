package status

import (
	"context"

	"connectrpc.com/connect"
	statusv1 "github.com/0utl1er-tech/phox-customer/gen/pb/status/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
)

func (s *StatusService) GetDefaultStatus(ctx context.Context, req *connect.Request[statusv1.GetDefaultStatusRequest]) (*connect.Response[statusv1.GetDefaultStatusResponse], error) {
	bookID, err := uuid.Parse(req.Msg.BookId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// 権限チェック
	err = s.authorizer.CheckPermission(ctx, bookID, db.RoleViewer)
	if err != nil {
		return nil, err
	}

	// priorityが一番小さいステータスを取得
	status, err := s.queries.GetDefaultStatusByBookID(ctx, bookID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&statusv1.GetDefaultStatusResponse{
		Status: convertToStatusPb(status),
	}), nil
}
