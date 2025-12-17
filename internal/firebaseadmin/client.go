package firebaseadmin

import (
	"context"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

type Client struct {
	authClient *auth.Client
}

func NewClient(ctx context.Context, credentialsFile string) (*Client, error) {
	var app *firebase.App
	var err error

	if credentialsFile != "" {
		app, err = firebase.NewApp(ctx, nil, option.WithCredentialsFile(credentialsFile))
	} else {
		app, err = firebase.NewApp(ctx, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to initialize firebase app: %w", err)
	}

	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get firebase auth client: %w", err)
	}

	return &Client{
		authClient: authClient,
	}, nil
}

type CreateUserParams struct {
	Email       string
	Password    string
	DisplayName string
}

func (c *Client) CreateUser(ctx context.Context, params CreateUserParams) (string, error) {
	userToCreate := (&auth.UserToCreate{}).
		Email(params.Email).
		Password(params.Password).
		DisplayName(params.DisplayName)

	userRecord, err := c.authClient.CreateUser(ctx, userToCreate)
	if err != nil {
		return "", fmt.Errorf("failed to create firebase user: %w", err)
	}

	return userRecord.UID, nil
}

func (c *Client) DeleteUser(ctx context.Context, uid string) error {
	err := c.authClient.DeleteUser(ctx, uid)
	if err != nil {
		return fmt.Errorf("failed to delete firebase user: %w", err)
	}
	return nil
}

func (c *Client) GetUserByEmail(ctx context.Context, email string) (*auth.UserRecord, error) {
	return c.authClient.GetUserByEmail(ctx, email)
}
