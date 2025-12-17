package user

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	userv1 "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

func (s *UserService) AddCompanyUser(
	ctx context.Context,
	req *connect.Request[userv1.AddCompanyUserRequest],
) (*connect.Response[userv1.AddCompanyUserResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	callerUser, err := s.queries.GetUser(ctx, token.Subject())
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("caller user not found: %w", err))
	}

	newUserID := req.Msg.GetUserId()
	if newUserID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("user_id is required"))
	}

	dbUser, err := s.queries.CreateUser(ctx, db.CreateUserParams{
		ID:        newUserID,
		CompanyID: callerUser.CompanyID,
		Name:      req.Msg.GetName(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create user: %w", err))
	}

	protoUser, err := s.convertUserToProto(ctx, dbUser)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert user: %w", err))
	}

	return connect.NewResponse(&userv1.AddCompanyUserResponse{
		User: protoUser,
	}), nil
}
