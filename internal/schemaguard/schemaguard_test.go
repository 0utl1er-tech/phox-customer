package schemaguard_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/0utl1er-tech/phox-customer/internal/schemaguard"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeMigrations(names ...string) fstest.MapFS {
	m := fstest.MapFS{}
	for _, n := range names {
		m["db/migration/"+n] = &fstest.MapFile{Data: []byte("-- noop")}
	}
	return m
}

func TestExpectedVersion(t *testing.T) {
	v, err := schemaguard.ExpectedVersion(fakeMigrations(
		"000000_init.up.sql",
		"000002_b.up.sql",
		"000010_zoom.up.sql",
		"000010_zoom.down.sql", // down は無視
		"notes.md",             // sql 以外は無視
	), "db/migration")
	require.NoError(t, err)
	assert.EqualValues(t, 10, v)
}

func TestExpectedVersion_Empty(t *testing.T) {
	_, err := schemaguard.ExpectedVersion(fakeMigrations("readme.md"), "db/migration")
	require.Error(t, err)
}

// 実 DB に対する Verify。CI では migrate 済み (= 最新 version, clean) の
// DB が来る前提。
func TestVerify(t *testing.T) {
	pool, _ := testutil.SetupTestDB(t)
	ctx := context.Background()

	t.Run("バイナリ期待がDBより新しい場合はエラー", func(t *testing.T) {
		err := schemaguard.Verify(ctx, pool, fakeMigrations("999999_future.up.sql"), "db/migration")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "migrate")
	})

	t.Run("DBがバイナリより新しい場合はwarnのみで通す", func(t *testing.T) {
		err := schemaguard.Verify(ctx, pool, fakeMigrations("000000_init.up.sql"), "db/migration")
		require.NoError(t, err)
	})
}
