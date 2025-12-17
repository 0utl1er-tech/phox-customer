package permit

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/google/uuid"
)

func (server *PermitService) AddBookUser(ctx context.Context, req *connect.Request[permitv1.AddBookUserRequest]) (*connect.Response[permitv1.AddBookUserResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	callerID := token.Subject()

	bookID := uuid.MustParse(req.Msg.BookId)
	targetUserID := req.Msg.UserId

	callerRole, err := server.queries.CheckUserRoleForBook(ctx, db.CheckUserRoleForBookParams{
		BookID: bookID,
		UserID: callerID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("このブックにアクセスする権限がありません"))
	}

	if callerRole != db.RoleOwner {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("ユーザーを追加するにはオーナー権限が必要です"))
	}

	callerUser, err := server.queries.GetUser(ctx, callerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get caller user: %w", err))
	}

	targetUser, err := server.queries.GetUser(ctx, targetUserID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("指定されたユーザーが見つかりません"))
	}

	if callerUser.CompanyID != targetUser.CompanyID {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("同じ会社のユーザーのみ追加できます"))
	}

	role := req.Msg.Role
	if role == permitv1.Role_ROLE_UNSPECIFIED {
		role = permitv1.Role_ROLE_VIEWER
	}
	if role == permitv1.Role_ROLE_OWNER {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("オーナー権限は付与できません"))
	}

	permitID := uuid.New()
	result, err := server.queries.CreatePermit(ctx, db.CreatePermitParams{
		ID:     permitID,
		BookID: bookID,
		UserID: targetUserID,
		Role:   util.ConvertProtoRoleToDBRole(role),
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("このユーザーは既にアクセス権を持っています"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create permit: %w", err))
	}

	return connect.NewResponse(&permitv1.AddBookUserResponse{
		Permit: &permitv1.Permit{
			Id:     result.ID.String(),
			BookId: result.BookID.String(),
			UserId: result.UserID,
			Role:   util.ConvertDBRoleToProtoRole(result.Role),
		},
	}), nil
}
