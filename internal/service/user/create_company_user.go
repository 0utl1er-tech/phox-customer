package user

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	userv1 "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/keycloakadmin"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/rs/zerolog/log"
)

func (s *UserService) CreateCompanyUser(
	ctx context.Context,
	req *connect.Request[userv1.CreateCompanyUserRequest],
) (*connect.Response[userv1.CreateCompanyUserResponse], error) {
	if s.keycloakAdmin == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("keycloak admin client not configured"))
	}

	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	callerUser, err := s.queries.GetUser(ctx, token.Subject())
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("caller user not found: %w", err))
	}

	email := strings.TrimSpace(req.Msg.GetEmail())
	password := req.Msg.GetPassword()
	name := strings.TrimSpace(req.Msg.GetName())

	if email == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("email is required"))
	}
	if len(password) < 6 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("password must be at least 6 characters"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	keycloakUID, err := s.keycloakAdmin.CreateUser(ctx, keycloakadmin.CreateUserParams{
		Email:       email,
		Password:    password,
		DisplayName: name,
	})
	if err != nil {
		switch {
		case errors.Is(err, keycloakadmin.ErrEmailExists):
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("email already exists"))
		case errors.Is(err, keycloakadmin.ErrInvalidEmail):
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid email format"))
		case errors.Is(err, keycloakadmin.ErrWeakPassword):
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("password is too weak"))
		case errors.Is(err, keycloakadmin.ErrUnauthorized):
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("keycloak admin credentials rejected: %w", err))
		default:
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create keycloak user: %w", err))
		}
	}

	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		if deleteErr := s.keycloakAdmin.DeleteUser(ctx, keycloakUID); deleteErr != nil {
			log.Error().Err(deleteErr).Str("keycloak_uid", keycloakUID).Msg("failed to rollback keycloak user after db transaction start failure")
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start transaction: %w", err))
	}
	defer tx.Rollback(ctx)

	txQueries := s.queries.WithTx(tx)

	dbUser, err := txQueries.CreateUser(ctx, db.CreateUserParams{
		ID:        keycloakUID,
		CompanyID: callerUser.CompanyID,
		Name:      name,
	})
	if err != nil {
		if deleteErr := s.keycloakAdmin.DeleteUser(ctx, keycloakUID); deleteErr != nil {
			log.Error().Err(deleteErr).Str("keycloak_uid", keycloakUID).Msg("failed to rollback keycloak user after db insert failure")
		}
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("user already exists in database"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create user in database: %w", err))
	}

	if err := tx.Commit(ctx); err != nil {
		if deleteErr := s.keycloakAdmin.DeleteUser(ctx, keycloakUID); deleteErr != nil {
			log.Error().Err(deleteErr).Str("keycloak_uid", keycloakUID).Msg("failed to rollback keycloak user after db commit failure")
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to commit transaction: %w", err))
	}

	protoUser, err := s.convertUserToProto(ctx, dbUser)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert user: %w", err))
	}

	return connect.NewResponse(&userv1.CreateCompanyUserResponse{
		User: protoUser,
	}), nil
}
