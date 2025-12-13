package status

import (
	"context"

	"connectrpc.com/connect"
	statusv1 "github.com/0utl1er-tech/phox-customer/gen/pb/status/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
)

func (s *StatusService) ListStatuses(ctx context.Context, req *connect.Request[statusv1.ListStatusesRequest]) (*connect.Response[statusv1.ListStatusesResponse], error) {
	bookID, err := uuid.Parse(req.Msg.BookId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// 権限チェック
	err = s.authorizer.CheckPermission(ctx, bookID, db.RoleViewer)
	if err != nil {
		return nil, err
	}

	statuses, err := s.queries.ListStatusesByBookID(ctx, bookID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	pbStatuses := make([]*statusv1.Status, len(statuses))
	for i, status := range statuses {
		pbStatuses[i] = convertToStatusPb(status)
	}

	return connect.NewResponse(&statusv1.ListStatusesResponse{
		Statuses: pbStatuses,
	}), nil
}

func convertToStatusPb(status db.Status) *statusv1.Status {
	return &statusv1.Status{
		Id:        status.ID.String(),
		BookId:    status.BookID.String(),
		Priority:  status.Priority,
		Name:      status.Name,
		Effective: status.Effective,
		Ng:        status.Ng,
	}
}
