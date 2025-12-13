package permit

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/google/uuid"
)

func (server *PermitService) GetPermit(ctx context.Context, req *connect.Request[permitv1.GetPermitRequest]) (*connect.Response[permitv1.GetPermitResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	bookID := uuid.MustParse(req.Msg.BookId)

	// ユーザーがbookに権限を持っているかチェック
	_, err = server.queries.GetBookByIDAndUserID(ctx, db.GetBookByIDAndUserIDParams{
		ID:     bookID,
		UserID: userID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("このbookにアクセスする権限がありません: %w", err))
	}

	// ユーザーに権限がある場合、すべてのpermitを取得
	permits, err := server.queries.ListPermits(ctx, bookID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get permit: %w", err))
	}

	permitsResponse := make([]*permitv1.Permit, len(permits))
	for i, permit := range permits {
		permitsResponse[i] = &permitv1.Permit{
			Id:     permit.ID.String(),
			BookId: permit.BookID.String(),
			UserId: permit.UserID,
			Role:   util.ConvertDBRoleToProtoRole(permit.Role),
		}
	}

	return connect.NewResponse(&permitv1.GetPermitResponse{
		Permits: permitsResponse,
	}), nil
}
