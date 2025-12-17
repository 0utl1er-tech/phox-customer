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

func (server *PermitService) ListBookUsers(ctx context.Context, req *connect.Request[permitv1.ListBookUsersRequest]) (*connect.Response[permitv1.ListBookUsersResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	callerID := token.Subject()

	bookID := uuid.MustParse(req.Msg.BookId)

	_, err = server.queries.CheckUserRoleForBook(ctx, db.CheckUserRoleForBookParams{
		BookID: bookID,
		UserID: callerID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("このブックにアクセスする権限がありません"))
	}

	permits, err := server.queries.ListPermitsWithUserInfo(ctx, bookID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list book users: %w", err))
	}

	users := make([]*permitv1.BookUser, len(permits))
	for i, permit := range permits {
		users[i] = &permitv1.BookUser{
			PermitId: permit.ID.String(),
			UserId:   permit.UserID,
			UserName: permit.UserName,
			Role:     util.ConvertDBRoleToProtoRole(permit.Role),
		}
	}

	return connect.NewResponse(&permitv1.ListBookUsersResponse{
		Users: users,
	}), nil
}
