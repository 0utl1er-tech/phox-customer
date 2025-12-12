package auth

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// Authorizer provides authorization logic.
type Authorizer struct {
	Queries *db.Queries
}

// NewAuthorizer creates a new Authorizer.
func NewAuthorizer(queries *db.Queries) *Authorizer {
	return &Authorizer{Queries: queries}
}

// AuthorizeUser retrieves the user's token from the context.
func AuthorizeUser(ctx context.Context) (jwt.Token, error) {
	token, ok := ctx.Value(AuthorizationPayloadKey).(jwt.Token)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing authorization payload in context"))
	}
	return token, nil
}

// CheckPermission checks if a user has the required role for a specific book.
// It implements a hierarchical role check (owner > editor > viewer).
func (a *Authorizer) CheckPermission(ctx context.Context, bookID uuid.UUID, requiredRole db.Role) error {
	token, err := AuthorizeUser(ctx)
	if err != nil {
		return err
	}

	permit, err := a.Queries.GetPermitByBookIDAndUserID(ctx, db.GetPermitByBookIDAndUserIDParams{
		BookID: bookID,
		UserID: token.Subject(),
	})
	if err != nil {
		return connect.NewError(connect.CodePermissionDenied, errors.New("you do not have access to this book"))
	}

	// Hierarchical role check
	hasPermission := false
	switch requiredRole {
	case db.RoleOwner:
		if permit.Role == db.RoleOwner {
			hasPermission = true
		}
	case db.RoleEditor:
		if permit.Role == db.RoleOwner || permit.Role == db.RoleEditor {
			hasPermission = true
		}
	case db.RoleViewer:
		if permit.Role == db.RoleOwner || permit.Role == db.RoleEditor || permit.Role == db.RoleViewer {
			hasPermission = true
		}
	}

	if !hasPermission {
		return connect.NewError(connect.CodePermissionDenied, errors.New("you do not have the required permission for this action"))
	}

	return nil
}
