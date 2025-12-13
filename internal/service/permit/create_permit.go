package permit

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	util "github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/google/uuid"
)

func (server *PermitService) CreatePermit(ctx context.Context, req *connect.Request[permitv1.CreatePermitRequest]) (*connect.Response[permitv1.CreatePermitResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

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
