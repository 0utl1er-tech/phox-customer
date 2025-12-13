package status

import (
	"context"

	"connectrpc.com/connect"
	statusv1 "github.com/0utl1er-tech/phox-customer/gen/pb/status/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *StatusService) UpdateStatus(ctx context.Context, req *connect.Request[statusv1.UpdateStatusRequest]) (*connect.Response[statusv1.UpdateStatusResponse], error) {
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

	params := db.UpdateStatusParams{
		ID: statusID,
	}

	if req.Msg.Name != nil {
		params.Name = pgtype.Text{String: *req.Msg.Name, Valid: true}
	}
	if req.Msg.Priority != nil {
		params.Priority = pgtype.Int4{Int32: *req.Msg.Priority, Valid: true}
	}
	if req.Msg.Effective != nil {
		params.Effective = pgtype.Bool{Bool: *req.Msg.Effective, Valid: true}
	}
	if req.Msg.Ng != nil {
		params.Ng = pgtype.Bool{Bool: *req.Msg.Ng, Valid: true}
	}

	updatedStatus, err := s.queries.UpdateStatus(ctx, params)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&statusv1.UpdateStatusResponse{
		Status: convertToStatusPb(updatedStatus),
	}), nil
}
