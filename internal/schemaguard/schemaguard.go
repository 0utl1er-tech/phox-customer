// Package schemaguard はバイナリが期待する DB スキーマ version と
// schema_migrations の実 version を起動時に突き合わせる fail-fast ガード。
//
// migration を実行するのは Docker の start.sh と CI だけで、`go run .` や
// air での直起動はスキーマが古いまま走れてしまう。その状態で書き込みに
// 行くと「column "duration_seconds" does not exist」のような不可解な
// SQLSTATE 42703 が初回 INSERT まで遅延して出る (実証: 2026-06-12)。
// このガードは起動時点で版ズレを検出し、修正コマンド付きで即死させる。
package schemaguard

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// migrateHint はエラーメッセージに添える修正コマンド。秘密情報を含まない
// よう DB URL は環境変数参照のまま提示する。
const migrateHint = `migrate -path db/migration -database "$DB_SOURCE" up  (CLI が無ければ: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest)`

// ExpectedVersion は migration ディレクトリの *.up.sql ファイル名
// (例: 000010_zoom_call_columns.up.sql) から最大 version を返す。
// up ファイルが 1 つも無ければエラー。
func ExpectedVersion(fsys fs.FS, dir string) (uint64, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return 0, fmt.Errorf("read migration dir %q: %w", dir, err)
	}
	var max uint64
	found := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			continue
		}
		v, err := strconv.ParseUint(prefix, 10, 64)
		if err != nil {
			continue
		}
		found = true
		if v > max {
			max = v
		}
	}
	if !found {
		return 0, fmt.Errorf("no *.up.sql migrations found in %q", dir)
	}
	return max, nil
}

// Verify はバイナリに埋め込まれた migration の期待 version と
// schema_migrations を比較する。
//
//   - schema_migrations が無い / version が古い / dirty → error (起動中止)
//   - version がバイナリより新しい → warn のみ (ローリングデプロイ中は
//     旧バイナリ + 新スキーマが一時的に共存するため、ここで殺してはいけない)
func Verify(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, dir string) error {
	expected, err := ExpectedVersion(fsys, dir)
	if err != nil {
		return err
	}

	var version uint64
	var dirty bool
	err = pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty)
	switch {
	case err == nil:
		// fall through
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("schema_migrations が空です (migration 未実行)。実行: %s", migrateHint)
	default:
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" { // undefined_table
			return fmt.Errorf("schema_migrations テーブルがありません (このDBは一度も migrate されていない)。実行: %s", migrateHint)
		}
		return fmt.Errorf("schema_migrations の確認に失敗: %w", err)
	}

	if dirty {
		return fmt.Errorf(
			"schema_migrations が dirty です (version %d で migration が失敗したまま)。"+
				"原因を直してから `migrate ... force %d` で版を戻し、再度 up を実行してください",
			version, version-1)
	}
	if version < expected {
		return fmt.Errorf(
			"DB スキーマが古いです (DB=version %d, バイナリ期待=version %d)。"+
				"このまま起動すると存在しない列への INSERT で SQLSTATE 42703 が出ます。実行: %s",
			version, expected, migrateHint)
	}
	if version > expected {
		log.Warn().
			Uint64("db_version", version).
			Uint64("binary_expected", expected).
			Msg("DB schema is newer than this binary (rolling deploy 中なら正常。続行します)")
	}
	return nil
}
