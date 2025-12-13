package status

import (
	"context"

	"connectrpc.com/connect"
	statusv1 "github.com/0utl1er-tech/phox-customer/gen/pb/status/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
)

func (s *StatusService) CreateStatus(ctx context.Context, req *connect.Request[statusv1.CreateStatusRequest]) (*connect.Response[statusv1.CreateStatusResponse], error) {
	bookID, err := uuid.Parse(req.Msg.BookId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// 権限チェック（editor以上が必要）
	err = s.authorizer.CheckPermission(ctx, bookID, db.RoleEditor)
	if err != nil {
		return nil, err
	}

	// 最大priorityを取得
	maxPriorityInterface, err := s.queries.GetMaxStatusPriority(ctx, bookID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var maxPriority int32 = 0
	if maxPriorityInterface != nil {
		switch v := maxPriorityInterface.(type) {
		case int32:
			maxPriority = v
		case int64:
			maxPriority = int32(v)
		case int:
			maxPriority = int32(v)
		}
	}

	newStatus, err := s.queries.CreateStatus(ctx, db.CreateStatusParams{
		ID:        uuid.New(),
		BookID:    bookID,
		Priority:  maxPriority + 1,
		Name:      req.Msg.Name,
		Effective: req.Msg.Effective,
		Ng:        req.Msg.Ng,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&statusv1.CreateStatusResponse{
		Status: convertToStatusPb(newStatus),
	}), nil
}
