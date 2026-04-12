// Package testutil はテスト用のユーティリティを提供する。
// real postgres に接続してテスト用のデータセットアップ・クリーンアップを行う。
package testutil

import (
	"context"
	"os"
	"testing"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultDBSource はテスト用 DB 接続文字列。
// 環境変数 TEST_DB_SOURCE で override 可能。
func DefaultDBSource() string {
	if s := os.Getenv("TEST_DB_SOURCE"); s != "" {
		return s
	}
	return "postgresql://root:secret@localhost:5432/phox-customer?sslmode=disable"
}

// SetupTestDB はテスト用の pgxpool + sqlc Queries を返す。
// テストの cleanup で pool を閉じる。
func SetupTestDB(t *testing.T) (*pgxpool.Pool, *db.Queries) {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, DefaultDBSource())
	if err != nil {
		t.Skipf("test DB not available: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("test DB ping failed: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool, db.New(pool)
}

// TestCompanyID はテスト用の Company ID。
// 000003_create_activity.up.sql で seed された company を返す。
// 無ければ作成する。
func TestCompanyID(t *testing.T, q *db.Queries) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	// 直接 SQL は使えないので、User 'system' の company_id を引く
	u, err := q.GetUser(ctx, "system")
	if err == nil {
		return u.CompanyID
	}
	// system がいなければ作る
	id = uuid.New()
	_, err = q.CreateCompany(ctx, db.CreateCompanyParams{
		ID:   id,
		Name: "test-company",
	})
	if err != nil {
		t.Fatalf("create test company: %v", err)
	}
	return id
}

// TestUser はテスト用の User を作成して返す。既に同 ID があればそれを返す。
func TestUser(t *testing.T, q *db.Queries, userID string, companyID uuid.UUID) db.User {
	t.Helper()
	ctx := context.Background()
	u, err := q.GetUser(ctx, userID)
	if err == nil {
		return u
	}
	u, err = q.CreateUser(ctx, db.CreateUserParams{
		ID:        userID,
		CompanyID: companyID,
		Name:      "test-user-" + userID,
	})
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return u
}

// TestBook はテスト用の Book + Permit (owner) を作成して返す。
func TestBook(t *testing.T, q *db.Queries, userID string) db.Book {
	t.Helper()
	ctx := context.Background()
	bookID := uuid.New()
	b, err := q.CreateBook(ctx, db.CreateBookParams{
		ID:   bookID,
		Name: "test-book-" + bookID.String()[:8],
	})
	if err != nil {
		t.Fatalf("create test book: %v", err)
	}
	_, err = q.CreatePermit(ctx, db.CreatePermitParams{
		ID:     uuid.New(),
		BookID: bookID,
		UserID: userID,
		Role:   db.RoleOwner,
	})
	if err != nil {
		t.Fatalf("create test permit: %v", err)
	}
	// Seed default status (Phase 20b)
	_, _ = q.CreateStatus(ctx, db.CreateStatusParams{
		ID:        uuid.New(),
		BookID:    bookID,
		Priority:  1,
		Name:      "未対応",
		Effective: false,
		Ng:        false,
	})
	return b
}

// TestCustomer はテスト用の Customer を作成して返す。
func TestCustomer(t *testing.T, q *db.Queries, bookID uuid.UUID) db.Customer {
	t.Helper()
	ctx := context.Background()
	c, err := q.CreateCustomer(ctx, db.CreateCustomerParams{
		ID:     uuid.New(),
		BookID: bookID,
		Name:   "テスト顧客-" + uuid.NewString()[:8],
		Phone:  "03-1234-5678",
		Mail:   "test-" + uuid.NewString()[:8] + "@example.com",
	})
	if err != nil {
		t.Fatalf("create test customer: %v", err)
	}
	return c
}
