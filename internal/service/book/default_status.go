package book

import (
	"context"
	"fmt"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
)

// DefaultStatusName は新規 Book に自動付与される初期 Status の名前。
// PhoneInput.saveCallHistory が GetDefaultStatus を呼ぶ時、この行が
// priority 最小として返り、CreateActivityCall の status_id に使われる。
//
// Status (book_id, name) には UNIQUE index があるので、二重作成は拒否される。
const DefaultStatusName = "未対応"

// SeedDefaultStatus は指定 Book に「未対応」Status を 1 行作成する。
// 既に同名の行があれば (UNIQUE 違反) は無視して呼び出し側に success を返す。
func SeedDefaultStatus(ctx context.Context, queries *db.Queries, bookID uuid.UUID) error {
	_, err := queries.CreateStatus(ctx, db.CreateStatusParams{
		ID:        uuid.New(),
		BookID:    bookID,
		Priority:  1,
		Name:      DefaultStatusName,
		Effective: false,
		Ng:        false,
	})
	if err != nil {
		// UNIQUE 違反はレース時に起こり得るので errors.Is では判定せず文字列で寛容に。
		// (pgx 側の PgError まで拾うのは将来のリファクタで OK)
		if isUniqueViolation(err) {
			return nil
		}
		return fmt.Errorf("seed default status: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx v5 では *pgconn.PgError で Code=="23505"
	// 簡潔のため文字列判定でカバーする (将来 pgconn Import に変更可)
	s := err.Error()
	return containsAny(s, "23505", "duplicate key", "unique constraint")
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		for i := 0; i+len(n) <= len(s); i++ {
			if s[i:i+len(n)] == n {
				return true
			}
		}
	}
	return false
}
