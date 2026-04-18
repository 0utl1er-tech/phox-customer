package user

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	userv1 "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/rs/zerolog/log"
)

// DeleteCompanyUser removes a user from the caller's company.
//
// Intent: clean up stale / imported-by-mistake entries from the company user
// list — this is a list-hygiene operation, not a security lockout. The
// Keycloak account is intentionally left untouched; if the target user
// still has valid credentials and logs into Phox again, the auth
// interceptor will JIT-provision them back in (as role=viewer). For a hard
// lockout, disable or delete the Keycloak account from the Keycloak admin
// UI instead.
//
// Constraints enforced:
//   - caller must have role=owner
//   - target user must exist and belong to the same company as the caller
//   - caller cannot delete themselves (avoid locking the company out)
//
// FK constraints on User are ON DELETE CASCADE, so Activity / Permit /
// Redial / UserGoogleToken / UserICalFeed rows for this user are cleaned
// up in the same statement — no manual bookkeeping required.
func (s *UserService) DeleteCompanyUser(
	ctx context.Context,
	req *connect.Request[userv1.DeleteCompanyUserRequest],
) (*connect.Response[userv1.DeleteCompanyUserResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	callerUser, err := s.queries.GetUser(ctx, token.Subject())
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("caller user not found: %w", err))
	}
	if callerUser.Role != "owner" {
		return nil, connect.NewError(connect.CodePermissionDenied,
			fmt.Errorf("only owners can delete company users"))
	}

	targetID := req.Msg.GetUserId()
	if targetID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("user_id is required"))
	}
	if targetID == callerUser.ID {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("cannot delete yourself"))
	}

	targetUser, err := s.queries.GetUser(ctx, targetID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("target user not found: %w", err))
	}
	if targetUser.CompanyID != callerUser.CompanyID {
		return nil, connect.NewError(connect.CodePermissionDenied,
			fmt.Errorf("cannot delete users from another company"))
	}

	if _, err := s.queries.DeleteUser(ctx, targetID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete phox user: %w", err))
	}

	log.Info().
		Str("user_id", targetID).
		Str("caller_id", callerUser.ID).
		Str("company_id", callerUser.CompanyID.String()).
		Msg("company user deleted")

	return connect.NewResponse(&userv1.DeleteCompanyUserResponse{UserId: targetID}), nil
}
