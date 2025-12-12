package permit

import (
	"context"
	"fmt"

	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	util "github.com/0utl1er-tech/phox-customer/internal/util"
	"connectrpc.com/connect"
	"github.com/google/uuid"
)

func (server *PermitService) CreatePermit(ctx context.Context, req *connect.Request[permitv1.CreatePermitRequest]) (*connect.Response[permitv1.CreatePermitResponse], error) {
	// Connect-GoのヘッダーからX-User-IDを取得
	userID := req.Header().Get("X-User-ID")
	if userID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("x-user-idがヘッダーに見つかりません"))
	}

	permitId := uuid.New()

	result, err := server.queries.CreatePermit(ctx, db.CreatePermitParams{
		ID:     permitId,
		BookID: uuid.MustParse(req.Msg.BookId),
		UserID: userID,
		Role:   util.ConvertProtoRoleToDBRole(req.Msg.Role),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create permit: %w", err))
	}

	return connect.NewResponse(&permitv1.CreatePermitResponse{
		CreatedPermit: &permitv1.Permit{
			Id:     result.ID.String(),
			BookId: result.BookID.String(),
			UserId: result.UserID,
			Role:   util.ConvertDBRoleToProtoRole(result.Role),
		},
	}), nil
}
