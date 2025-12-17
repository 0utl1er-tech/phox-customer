package user

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	userv1 "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

func (s *UserService) ListCompanyUsers(
	ctx context.Context,
	req *connect.Request[userv1.ListCompanyUsersRequest],
) (*connect.Response[userv1.ListCompanyUsersResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	callerUser, err := s.queries.GetUser(ctx, token.Subject())
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("caller user not found: %w", err))
	}

	dbUsers, err := s.queries.ListUsersByCompany(ctx, callerUser.CompanyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list users: %w", err))
	}

	protoUsers := make([]*userv1.User, 0, len(dbUsers))
	for _, dbUser := range dbUsers {
		protoUser, err := s.convertUserToProto(ctx, dbUser)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert user: %w", err))
		}
		protoUsers = append(protoUsers, protoUser)
	}

	return connect.NewResponse(&userv1.ListCompanyUsersResponse{
		Users: protoUsers,
	}), nil
}
