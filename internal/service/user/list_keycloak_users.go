package user

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	userv1 "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

// defaultListMax is used when the request omits `max` (or sets it to 0).
// Keycloak's GetUsers also enforces its own upper bound, but we clamp here
// so oversized requests don't blow up UI rendering either.
const (
	defaultListMax = 100
	maxListMax     = 500
)

// ListKeycloakUsers enumerates users in the configured Keycloak realm and
// annotates each with a `linked` flag based on the presence of a Phox DB
// User row. Used by the settings UI to drive the "import from Keycloak"
// workflow. Caller must have role=owner.
//
// Unlike ListCompanyUsers this hits Keycloak directly, not just the phox DB,
// so a user that exists in Keycloak but has never logged into Phox (and
// therefore hasn't been JIT-provisioned) still shows up with linked=false.
func (s *UserService) ListKeycloakUsers(
	ctx context.Context,
	req *connect.Request[userv1.ListKeycloakUsersRequest],
) (*connect.Response[userv1.ListKeycloakUsersResponse], error) {
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
			fmt.Errorf("only owners can list Keycloak users"))
	}

	max := int(req.Msg.GetMax())
	if max <= 0 {
		max = defaultListMax
	}
	if max > maxListMax {
		max = maxListMax
	}

	kcUsers, err := s.keycloakAdmin.ListUsers(ctx, req.Msg.GetSearch(), max)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list Keycloak users: %w", err))
	}

	// Build an id set of Phox-linked users in the same company so we can
	// annotate each Keycloak user with the `linked` flag cheaply.
	dbUsers, err := s.queries.ListUsersByCompany(ctx, callerUser.CompanyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list phox users: %w", err))
	}
	linked := make(map[string]struct{}, len(dbUsers))
	for _, u := range dbUsers {
		linked[u.ID] = struct{}{}
	}

	protoUsers := make([]*userv1.KeycloakUser, 0, len(kcUsers))
	for _, u := range kcUsers {
		ku := &userv1.KeycloakUser{
			Id:        strPtr(u.ID),
			Username:  strPtr(u.Username),
			Email:     strPtr(u.Email),
			FirstName: strPtr(u.FirstName),
			LastName:  strPtr(u.LastName),
		}
		if _, ok := linked[ku.Id]; ok {
			ku.Linked = true
		}
		protoUsers = append(protoUsers, ku)
	}

	return connect.NewResponse(&userv1.ListKeycloakUsersResponse{
		Users: protoUsers,
	}), nil
}

// strPtr safely dereferences a *string returned by gocloak, returning "" for
// nil. Keycloak users that lack optional attributes (no email, no first name)
// come back with nil pointers, which panic if dereferenced directly.
func strPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
