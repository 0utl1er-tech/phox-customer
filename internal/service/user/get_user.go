package user

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	userv1 "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

func (s *UserService) GetUser(
	ctx context.Context,
	req *connect.Request[userv1.GetUserRequest],
) (*connect.Response[userv1.GetUserResponse], error) {
	_, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	dbUser, err := s.queries.GetUser(ctx, req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("user not found: %w", err))
	}

	protoUser, err := s.convertUserToProto(ctx, dbUser)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert user: %w", err))
	}

	return connect.NewResponse(&userv1.GetUserResponse{
		User: protoUser,
	}), nil
}

func (s *UserService) GetMe(
	ctx context.Context,
	req *connect.Request[userv1.GetMeRequest],
) (*connect.Response[userv1.GetMeResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	dbUser, err := s.queries.GetUser(ctx, token.Subject())
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("user not found: %w", err))
	}

	protoUser, err := s.convertUserToProto(ctx, dbUser)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert user: %w", err))
	}

	return connect.NewResponse(&userv1.GetMeResponse{
		User: protoUser,
	}), nil
}
