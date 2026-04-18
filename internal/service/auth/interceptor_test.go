package auth

import (
	"context"
	"os"
	"testing"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// setupDB returns a live sqlc Queries against a test postgres; the test is
// skipped if the DB is unreachable so `go test ./...` still passes in the
// default sandbox. Mirrors internal/testutil/db.go — inlined here to avoid
// an import cycle (testutil imports auth for AuthorizationPayloadKey).
func setupDB(t *testing.T) *db.Queries {
	t.Helper()
	source := os.Getenv("TEST_DB_SOURCE")
	if source == "" {
		source = "postgresql://root:secret@localhost:5432/phox-customer?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, source)
	if err != nil {
		t.Skipf("test DB not available: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("test DB ping failed: %v", err)
	}
	t.Cleanup(pool.Close)
	return db.New(pool)
}

// ensureCompany picks the first Company row (single-tenant seed) or creates
// a throwaway test company if none exist.
func ensureCompany(t *testing.T, q *db.Queries) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	companies, err := q.ListCompanies(ctx)
	if err == nil && len(companies) > 0 {
		return companies[0].ID
	}
	id := uuid.New()
	if _, err := q.CreateCompany(ctx, db.CreateCompanyParams{ID: id, Name: "auth-test"}); err != nil {
		t.Fatalf("create test company: %v", err)
	}
	return id
}

// TestJITProvisionUser_Creates confirms that a verified token for a new `sub`
// results in a DB User row with role=viewer (the SQL default).
//
// We build the Interceptor struct by hand here so we don't have to stand up
// a JWK server — the JIT path only needs Queries and defaultCompanyID.
func TestJITProvisionUser_Creates(t *testing.T) {
	q := setupDB(t)
	ctx := context.Background()
	companyID := ensureCompany(t, q)

	i := &Interceptor{Queries: q, defaultCompanyID: companyID}

	sub := "kc-" + uuid.NewString() // fresh every run so we hit the INSERT path
	tok, err := jwt.NewBuilder().
		Subject(sub).
		Claim("name", "山田 太郎").
		Claim("preferred_username", "yamada").
		Claim("email", "yamada@example.com").
		IssuedAt(time.Now()).
		Expiration(time.Now().Add(time.Hour)).
		Build()
	if err != nil {
		t.Fatalf("build token: %v", err)
	}

	user, err := i.jitProvisionUser(ctx, tok)
	if err != nil {
		t.Fatalf("jitProvisionUser: %v", err)
	}
	if user.ID != sub {
		t.Errorf("ID: want %q, got %q", sub, user.ID)
	}
	if user.Name != "山田 太郎" {
		t.Errorf("Name: want 'name' claim, got %q", user.Name)
	}
	if user.Role != db.RoleViewer {
		t.Errorf("Role: want viewer (SQL default), got %q", user.Role)
	}
	if user.CompanyID != companyID {
		t.Errorf("CompanyID: want %s, got %s", companyID, user.CompanyID)
	}
}

// TestJITProvisionUser_RaceReusesExisting exercises the UNIQUE-violation
// recovery branch: pre-create a row, then invoke JIT — the INSERT fails with
// 23505, isUniqueViolation catches it, and we return the pre-existing row.
func TestJITProvisionUser_RaceReusesExisting(t *testing.T) {
	q := setupDB(t)
	ctx := context.Background()
	companyID := ensureCompany(t, q)

	sub := "kc-race-" + uuid.NewString()
	pre, err := q.CreateUser(ctx, db.CreateUserParams{
		ID:        sub,
		CompanyID: companyID,
		Name:      "pre-existing",
	})
	if err != nil {
		t.Fatalf("pre-create: %v", err)
	}

	i := &Interceptor{Queries: q, defaultCompanyID: companyID}
	tok, _ := jwt.NewBuilder().
		Subject(sub).
		Claim("name", "ignored by race branch").
		IssuedAt(time.Now()).
		Expiration(time.Now().Add(time.Hour)).
		Build()

	user, err := i.jitProvisionUser(ctx, tok)
	if err != nil {
		t.Fatalf("expected race branch to recover, got: %v", err)
	}
	if user.ID != pre.ID || user.Name != pre.Name {
		t.Errorf("race branch returned wrong row: want %+v, got %+v", pre, user)
	}
}

// TestExtractNameClaim covers the claim fallback ladder used by JIT. Pure
// unit test — no DB needed.
func TestExtractNameClaim(t *testing.T) {
	cases := []struct {
		desc   string
		claims map[string]interface{}
		sub    string
		want   string
	}{
		{
			desc:   "prefers name claim",
			claims: map[string]interface{}{"name": "Full Name", "preferred_username": "pu", "email": "e@x"},
			sub:    "sub-1",
			want:   "Full Name",
		},
		{
			desc:   "falls back to preferred_username",
			claims: map[string]interface{}{"preferred_username": "pu", "email": "e@x"},
			sub:    "sub-2",
			want:   "pu",
		},
		{
			desc:   "falls back to email",
			claims: map[string]interface{}{"email": "e@x"},
			sub:    "sub-3",
			want:   "e@x",
		},
		{
			desc:   "ignores empty/whitespace",
			claims: map[string]interface{}{"name": "   ", "preferred_username": "ok"},
			sub:    "sub-4",
			want:   "ok",
		},
		{
			desc:   "last-resort falls back to sub",
			claims: map[string]interface{}{},
			sub:    "sub-5",
			want:   "sub-5",
		},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			b := jwt.NewBuilder().Subject(c.sub).
				IssuedAt(time.Now()).
				Expiration(time.Now().Add(time.Hour))
			for k, v := range c.claims {
				b = b.Claim(k, v)
			}
			tok, _ := b.Build()
			got := extractNameClaim(tok)
			if got != c.want {
				t.Errorf("want %q, got %q", c.want, got)
			}
		})
	}
}
