package user

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	userv1 "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
)

func (s *UserService) CreateUser(
	ctx context.Context,
	req *connect.Request[userv1.CreateUserRequest],
) (*connect.Response[userv1.CreateUserResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid company_id: %w", err))
	}

	dbUser, err := s.queries.CreateUser(ctx, db.CreateUserParams{
		ID:        token.Subject(),
		CompanyID: companyID,
		Name:      req.Msg.GetName(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create user: %w", err))
	}

	protoUser, err := s.convertUserToProto(ctx, dbUser)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert user: %w", err))
	}

	return connect.NewResponse(&userv1.CreateUserResponse{
		User: protoUser,
	}), nil
}
