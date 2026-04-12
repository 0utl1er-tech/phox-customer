//go:build gcal_integration
// +build gcal_integration

package gcal

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func mustPool(t *testing.T, ctx context.Context, src string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, src)
	if err != nil {
		t.Fatalf("pgx pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("db ping: %v", err)
	}
	return pool
}

// getTestCompanyID returns the id of the first Company row, creating one if none exists.
// integration test は既存 seed を前提にしないので、fallback として挿入する。
func getTestCompanyID(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	row := pool.QueryRow(ctx, `SELECT id FROM "Company" LIMIT 1`)
	if err := row.Scan(&id); err == nil {
		return id
	}
	// No company — create one
	id = uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO "Company" (id, name) VALUES ($1, 'gcal-integration-company')`, id); err != nil {
		t.Fatalf("create company: %v", err)
	}
	return id
}
