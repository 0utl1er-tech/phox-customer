package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// AuthContext は指定 userID + email で認証済みの context を返す。
// サービスの auth.AuthorizeUser(ctx) がこの context からトークンを取り出せる。
func AuthContext(t *testing.T, userID, email string) context.Context {
	t.Helper()
	tok, err := jwt.NewBuilder().
		Subject(userID).
		IssuedAt(time.Now()).
		Expiration(time.Now().Add(time.Hour)).
		Claim("email", email).
		Claim("email_verified", true).
		Build()
	if err != nil {
		t.Fatalf("build test token: %v", err)
	}
	return context.WithValue(context.Background(), auth.AuthorizationPayloadKey, tok)
}
