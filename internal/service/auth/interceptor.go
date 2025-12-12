package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/rs/zerolog/log"
)

const (
	authorizationHeader     = "authorization"
	authorizationBearer     = "bearer"
	AuthorizationPayloadKey = "authorization_payload"
)

// Interceptor is a struct that holds dependencies for authentication.
type Interceptor struct {
	Queries  *db.Queries
	Config   util.Config
	jwkCache *jwk.Cache
}

// NewAuthInterceptor creates a new AuthInterceptor.
func NewAuthInterceptor(ctx context.Context, queries *db.Queries, config util.Config) *Interceptor {
	// Set up a background context for the JWK cache
	bgCtx, cancel := context.WithCancel(ctx)

	// Create a new JWK cache
	cache := jwk.NewCache(bgCtx)

	// Register the JWKS URL for caching
	err := cache.Register(config.JWTJwksURL, jwk.WithMinRefreshInterval(15*time.Minute))
	if err != nil {
		cancel()
		log.Fatal().Err(err).Msg("Failed to register JWKS URL")
	}

	// Trigger initial fetch
	_, err = cache.Refresh(bgCtx, config.JWTJwksURL)
	if err != nil {
		// Log as a warning instead of fatal, as it might recover
		log.Warn().Err(err).Msg("Failed to perform initial JWKS refresh")
	}

	// Ensure the cache is cleaned up when the context is done
	go func() {
		<-bgCtx.Done()
		cancel()
	}()

	return &Interceptor{
		Queries:  queries,
		Config:   config,
		jwkCache: cache,
	}
}

// WrapUnary creates a new unary interceptor for authentication and authorization.
func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		authHeader := req.Header().Get(authorizationHeader)
		if authHeader == "" {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authorization header is not provided"))
		}

		fields := strings.Fields(authHeader)
		if len(fields) < 2 {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid authorization header format"))
		}

		authType := strings.ToLower(fields[0])
		if authType != authorizationBearer {
			return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("unsupported authorization type: %s", authType))
		}

		accessToken := fields[1]
		token, err := i.verifyToken(ctx, accessToken)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("invalid access token: %w", err))
		}

		// Check if user exists in the database
		_, err = i.Queries.GetUser(ctx, token.Subject())
		if err != nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("user not found in database"))
		}

		ctx = context.WithValue(ctx, AuthorizationPayloadKey, token)
		return next(ctx, req)
	}
}

// WrapStreamingClient implements the connect.Interceptor interface for streaming client RPCs.
func (i *Interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		// Note: Streaming client authentication would need to be implemented here if needed
		return next(ctx, spec)
	}
}

// WrapStreamingHandler implements the connect.Interceptor interface for streaming handler RPCs.
func (i *Interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		// Note: Streaming handler authentication would need to be implemented here if needed
		return next(ctx, conn)
	}
}

// verifyToken verifies the access token using the JWKS cache.
func (i *Interceptor) verifyToken(ctx context.Context, tokenString string) (jwt.Token, error) {
	keySet, err := i.jwkCache.Get(ctx, i.Config.JWTJwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get JWK keyset: %w", err)
	}

	token, err := jwt.Parse([]byte(tokenString),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
		jwt.WithIssuer(i.Config.JWTIssuerURL),
		jwt.WithAudience(i.Config.JWTProjectID), // For Firebase, audience is the project ID
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse or validate token: %w", err)
	}

	return token, nil
}
