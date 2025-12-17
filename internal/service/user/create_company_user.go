package user

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	userv1 "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/firebaseadmin"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/rs/zerolog/log"
)

func (s *UserService) CreateCompanyUser(
	ctx context.Context,
	req *connect.Request[userv1.CreateCompanyUserRequest],
) (*connect.Response[userv1.CreateCompanyUserResponse], error) {
	if s.firebaseAuth == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("firebase admin client not configured"))
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

	firebaseUID, err := s.firebaseAuth.CreateUser(ctx, firebaseadmin.CreateUserParams{
		Email:       email,
		Password:    password,
		DisplayName: name,
	})
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "EMAIL_EXISTS") || strings.Contains(errStr, "email-already-exists") {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("email already exists"))
		}
		if strings.Contains(errStr, "INVALID_EMAIL") || strings.Contains(errStr, "invalid-email") {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid email format"))
		}
		if strings.Contains(errStr, "WEAK_PASSWORD") || strings.Contains(errStr, "weak-password") {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("password is too weak"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create firebase user: %w", err))
	}

	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		if deleteErr := s.firebaseAuth.DeleteUser(ctx, firebaseUID); deleteErr != nil {
			log.Error().Err(deleteErr).Str("firebase_uid", firebaseUID).Msg("failed to rollback firebase user after db transaction start failure")
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start transaction: %w", err))
	}
	defer tx.Rollback(ctx)

	txQueries := s.queries.WithTx(tx)

	dbUser, err := txQueries.CreateUser(ctx, db.CreateUserParams{
		ID:        firebaseUID,
		CompanyID: callerUser.CompanyID,
		Name:      name,
	})
	if err != nil {
		if deleteErr := s.firebaseAuth.DeleteUser(ctx, firebaseUID); deleteErr != nil {
			log.Error().Err(deleteErr).Str("firebase_uid", firebaseUID).Msg("failed to rollback firebase user after db insert failure")
		}
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("user already exists in database"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create user in database: %w", err))
	}

	if err := tx.Commit(ctx); err != nil {
		if deleteErr := s.firebaseAuth.DeleteUser(ctx, firebaseUID); deleteErr != nil {
			log.Error().Err(deleteErr).Str("firebase_uid", firebaseUID).Msg("failed to rollback firebase user after db commit failure")
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
