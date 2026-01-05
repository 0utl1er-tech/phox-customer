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
	bgCtx    context.Context
}

// NewAuthInterceptor creates a new AuthInterceptor.
func NewAuthInterceptor(ctx context.Context, queries *db.Queries, config util.Config) *Interceptor {
	// Use context.Background() for the JWK cache to ensure it persists
	bgCtx := context.Background()

	// Create a new JWK cache
	cache := jwk.NewCache(bgCtx)

	// Register the JWKS URL for caching with refresh options
	err := cache.Register(config.JWTJwksURL,
		jwk.WithMinRefreshInterval(15*time.Minute),
		jwk.WithRefreshInterval(60*time.Minute),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to register JWKS URL")
	}

	// Trigger initial fetch - retry if it fails
	maxRetries := 3
	var refreshErr error
	for i := 0; i < maxRetries; i++ {
		_, refreshErr = cache.Refresh(bgCtx, config.JWTJwksURL)
		if refreshErr == nil {
			log.Info().Msg("Successfully fetched JWK keyset")
			break
		}
		log.Warn().Err(refreshErr).Int("attempt", i+1).Msg("Failed to fetch JWK keyset, retrying...")
		time.Sleep(time.Second * time.Duration(i+1))
	}

	if refreshErr != nil {
		log.Fatal().Err(refreshErr).Msg("Failed to perform initial JWKS refresh after retries")
	}

	return &Interceptor{
		Queries:  queries,
		Config:   config,
		jwkCache: cache,
		bgCtx:    bgCtx,
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

		// Log token subject for debugging
		log.Debug().
			Str("subject", token.Subject()).
			Str("issuer", token.Issuer()).
			Msg("Token verified successfully")

		// Check if user exists in the database
		user, err := i.Queries.GetUser(ctx, token.Subject())
		if err != nil {
			log.Warn().
				Err(err).
				Str("token_subject", token.Subject()).
				Msg("Failed to get user from database")
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("user not found in database"))
		}

		log.Debug().
			Str("user_id", user.ID).
			Str("name", user.Name).
			Msg("User found in database")

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
	// Use the background context for cache operations to ensure persistence
	keySet, err := i.jwkCache.Get(i.bgCtx, i.Config.JWTJwksURL)
	if err != nil {
		// If cache is empty, try to refresh it
		log.Warn().Err(err).Msg("Failed to get JWK keyset from cache, attempting refresh")
		_, refreshErr := i.jwkCache.Refresh(i.bgCtx, i.Config.JWTJwksURL)
		if refreshErr != nil {
			return nil, fmt.Errorf("failed to get JWK keyset: %w (refresh error: %v)", err, refreshErr)
		}

		// Retry getting the keyset after refresh
		keySet, err = i.jwkCache.Get(i.bgCtx, i.Config.JWTJwksURL)
		if err != nil {
			return nil, fmt.Errorf("failed to get JWK keyset after refresh: %w", err)
		}
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
