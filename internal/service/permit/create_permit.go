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

func (server *PermitService) CreatePermit(ctx context.Context, req *connect.Request[permitv1.CreatePermitRequest]) (*connect.Response[permitv1.CreatePermitResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	bookID, err := util.ParseUUID("book_id", req.Msg.BookId)
	if err != nil {
		return nil, err
	}

	result, err := server.queries.CreatePermit(ctx, db.CreatePermitParams{
		ID:     uuid.New(),
		BookID: bookID,
		UserID: userID,
		Role:   util.ConvertProtoRoleToDBRole(req.Msg.Role),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create permit: %w", err))
	}

	return connect.NewResponse(&permitv1.CreatePermitResponse{
		CreatedPermit: modelToProto(result),
	}), nil
}
