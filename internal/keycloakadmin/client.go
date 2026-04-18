// Package keycloakadmin wraps the Keycloak Admin REST API (via gocloak) with
// the minimal surface that phox-customer needs for user provisioning.
//
// It intentionally exposes the same shape as the old firebaseadmin package so
// the rest of the codebase doesn't care which IdP is behind it.
package keycloakadmin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/Nerzal/gocloak/v13"
)

// Sentinel errors returned by CreateUser. Callers should use errors.Is.
var (
	ErrEmailExists   = errors.New("keycloakadmin: email already exists")
	ErrInvalidEmail  = errors.New("keycloakadmin: invalid email")
	ErrWeakPassword  = errors.New("keycloakadmin: password does not meet policy")
	ErrUserNotFound  = errors.New("keycloakadmin: user not found")
	ErrUnauthorized  = errors.New("keycloakadmin: admin credentials rejected")
	ErrKeycloakError = errors.New("keycloakadmin: upstream Keycloak error")
)

type Client struct {
	gocloak      *gocloak.GoCloak
	realm        string
	clientID     string
	clientSecret string
}

type CreateUserParams struct {
	Email       string
	Password    string
	DisplayName string
}

// NewClient validates config and returns a ready-to-use admin client.
// It does NOT perform a login probe — auth is done lazily per call so a Keycloak
// restart doesn't require restarting phox-customer.
func NewClient(_ context.Context, cfg util.Config) (*Client, error) {
	if cfg.KeycloakURL == "" {
		return nil, errors.New("keycloakadmin: KEYCLOAK_URL is not set")
	}
	if cfg.KeycloakRealm == "" {
		return nil, errors.New("keycloakadmin: KEYCLOAK_REALM is not set")
	}
	if cfg.KeycloakAdminClientID == "" {
		return nil, errors.New("keycloakadmin: KEYCLOAK_ADMIN_CLIENT_ID is not set")
	}
	if cfg.KeycloakAdminClientSecret == "" {
		return nil, errors.New("keycloakadmin: KEYCLOAK_ADMIN_CLIENT_SECRET is not set")
	}

	gc := gocloak.NewClient(cfg.KeycloakURL)

	return &Client{
		gocloak:      gc,
		realm:        cfg.KeycloakRealm,
		clientID:     cfg.KeycloakAdminClientID,
		clientSecret: cfg.KeycloakAdminClientSecret,
	}, nil
}

// adminToken gets a fresh client_credentials token. Tokens are short-lived and
// gocloak is cheap, so we don't bother caching.
func (c *Client) adminToken(ctx context.Context) (string, error) {
	token, err := c.gocloak.LoginClient(ctx, c.clientID, c.clientSecret, c.realm)
	if err != nil {
		return "", fmt.Errorf("%w: login failed: %v", ErrUnauthorized, err)
	}
	return token.AccessToken, nil
}

// CreateUser provisions a new user in Keycloak with the given email/password
// and returns the Keycloak `sub` (UUID string) suitable for use as the
// application-side User.id.
func (c *Client) CreateUser(ctx context.Context, params CreateUserParams) (string, error) {
	token, err := c.adminToken(ctx)
	if err != nil {
		return "", err
	}

	enabled := true
	emailVerified := true
	// Put the full display name into firstName so Keycloak's computed `name`
	// claim (= firstName + " " + lastName) matches what the frontend expects.
	// Keeping lastName empty is fine.
	firstName := params.DisplayName
	email := params.Email

	user := gocloak.User{
		Username:      &email,
		Email:         &email,
		Enabled:       &enabled,
		EmailVerified: &emailVerified,
		FirstName:     &firstName,
	}

	uid, err := c.gocloak.CreateUser(ctx, token, c.realm, user)
	if err != nil {
		return "", mapCreateError(err)
	}

	if err := c.gocloak.SetPassword(ctx, token, uid, c.realm, params.Password, false); err != nil {
		// Roll back the user so we don't leave a passwordless account around.
		_ = c.gocloak.DeleteUser(ctx, token, c.realm, uid)
		return "", mapCreateError(err)
	}

	return uid, nil
}

func (c *Client) DeleteUser(ctx context.Context, uid string) error {
	token, err := c.adminToken(ctx)
	if err != nil {
		return err
	}
	if err := c.gocloak.DeleteUser(ctx, token, c.realm, uid); err != nil {
		if apiErr, ok := err.(*gocloak.APIError); ok && apiErr.Code == http.StatusNotFound {
			return ErrUserNotFound
		}
		return fmt.Errorf("%w: delete user %s: %v", ErrKeycloakError, uid, err)
	}
	return nil
}

func (c *Client) GetUserByEmail(ctx context.Context, email string) (*gocloak.User, error) {
	token, err := c.adminToken(ctx)
	if err != nil {
		return nil, err
	}
	params := gocloak.GetUsersParams{Email: &email, Exact: gocloak.BoolP(true)}
	users, err := c.gocloak.GetUsers(ctx, token, c.realm, params)
	if err != nil {
		return nil, fmt.Errorf("%w: get users: %v", ErrKeycloakError, err)
	}
	if len(users) == 0 {
		return nil, ErrUserNotFound
	}
	return users[0], nil
}

// ListUsers returns users in the realm, optionally filtered by a free-text
// search term (matched against username/email/first/last name by Keycloak).
// max is the upper bound on rows returned; it's clamped by the caller.
func (c *Client) ListUsers(ctx context.Context, search string, max int) ([]*gocloak.User, error) {
	token, err := c.adminToken(ctx)
	if err != nil {
		return nil, err
	}
	params := gocloak.GetUsersParams{Max: &max}
	if s := strings.TrimSpace(search); s != "" {
		params.Search = &s
	}
	users, err := c.gocloak.GetUsers(ctx, token, c.realm, params)
	if err != nil {
		return nil, fmt.Errorf("%w: list users: %v", ErrKeycloakError, err)
	}
	return users, nil
}

// mapCreateError translates gocloak's HTTP errors into our sentinels.
func mapCreateError(err error) error {
	apiErr, ok := err.(*gocloak.APIError)
	if !ok {
		return fmt.Errorf("%w: %v", ErrKeycloakError, err)
	}
	msg := strings.ToLower(apiErr.Message)
	switch apiErr.Code {
	case http.StatusConflict:
		return ErrEmailExists
	case http.StatusBadRequest:
		if strings.Contains(msg, "password") {
			return ErrWeakPassword
		}
		if strings.Contains(msg, "email") {
			return ErrInvalidEmail
		}
		return fmt.Errorf("%w: %s", ErrKeycloakError, apiErr.Message)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrUnauthorized, apiErr.Message)
	default:
		return fmt.Errorf("%w: status=%d: %s", ErrKeycloakError, apiErr.Code, apiErr.Message)
	}
}
