package user

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	userv1 "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *UserService) UpdateUser(
	ctx context.Context,
	req *connect.Request[userv1.UpdateUserRequest],
) (*connect.Response[userv1.UpdateUserResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	params := db.UpdateUserParams{
		ID: token.Subject(),
	}

	if req.Msg.Name != nil {
		params.Name = pgtype.Text{String: *req.Msg.Name, Valid: true}
	}

	if req.Msg.CompanyId != nil {
		companyID, err := uuid.Parse(*req.Msg.CompanyId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid company_id: %w", err))
		}
		params.CompanyID = pgtype.UUID{Bytes: companyID, Valid: true}
	}

	dbUser, err := s.queries.UpdateUser(ctx, params)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update user: %w", err))
	}

	protoUser, err := s.convertUserToProto(ctx, dbUser)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert user: %w", err))
	}

	return connect.NewResponse(&userv1.UpdateUserResponse{
		User: protoUser,
	}), nil
}
