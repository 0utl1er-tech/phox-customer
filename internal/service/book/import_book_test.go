package book_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/book"
)

// fakeDBTX は sqlc の DBTX を満たす最小 fake。ImportBook が使うクエリは
// すべて QueryRow → Scan なので、Scan が no-op の row を返せば動く。
// Customer INSERT だけは ID を記録し、同一 ID の二回目で重複キーエラーを
// 返して挿入段階の失敗を再現する。
type fakeDBTX struct {
	insertedCustomerIDs map[uuid.UUID]bool
}

type fakeRow struct{ err error }

func (r fakeRow) Scan(dest ...any) error { return r.err }

func (f *fakeDBTX) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeDBTX) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return nil, errors.New("fakeDBTX: Query is not supported")
}

func (f *fakeDBTX) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if strings.Contains(sql, `INSERT INTO "Customer"`) {
		id := args[0].(uuid.UUID)
		if f.insertedCustomerIDs[id] {
			return fakeRow{err: errors.New(`duplicate key value violates unique constraint "Customer_pkey"`)}
		}
		f.insertedCustomerIDs[id] = true
	}
	return fakeRow{}
}

func (f *fakeDBTX) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("fakeDBTX: CopyFrom is not supported")
}

// ImportBook の集計・行番号の回帰テスト:
//   - imported_count はパース段階のエラーで二重に減らされない
//     (挿入に成功した件数そのもの)
//   - 挿入段階のエラーはパースでスキップされた行があっても
//     CSV の実際の行番号を指す
func TestImportBook_CountsAndLineNumbers(t *testing.T) {
	fake := &fakeDBTX{insertedCustomerIDs: map[uuid.UUID]bool{}}
	svc := book.NewBookService(db.New(fake), nil) // indexer nil → ES 連携はスキップ

	// 行 2: OK (ID 自動採番)
	// 行 3: パースエラー (invalid id)
	// 行 4: OK (明示 ID)
	// 行 5: 挿入エラー (行 4 と同じ ID → 重複キー)
	dupID := uuid.New().String()
	csv := "name,id\n" +
		"Alice,\n" +
		"Bob,not-a-uuid\n" +
		"Carol," + dupID + "\n" +
		"Dave," + dupID + "\n"

	resp, err := svc.ImportBook(context.Background(), connect.NewRequest(&bookv1.ImportBookRequest{
		FileName:    "import-test.csv",
		FileContent: []byte(csv),
		OwnerId:     "import-book-test-owner",
	}))
	require.NoError(t, err)

	// 挿入に成功したのは Alice と Carol の 2 件。バグ時は
	// len(customersToInsert) - len(importErrors) = 3 - 2 = 1 になっていた。
	assert.Equal(t, int32(2), resp.Msg.GetImportedCount())
	assert.Equal(t, int32(2), resp.Msg.GetFailedCount())

	require.Len(t, resp.Msg.GetErrors(), 2)
	parseErr, insertErr := resp.Msg.GetErrors()[0], resp.Msg.GetErrors()[1]

	assert.Equal(t, int32(3), parseErr.GetLineNumber())
	assert.Contains(t, parseErr.GetErrorMessage(), "invalid customer ID")

	// バグ時は customersToInsert の添字ベース (i+2 = 4) になっていた。
	assert.Equal(t, int32(5), insertErr.GetLineNumber())
	assert.Contains(t, insertErr.GetErrorMessage(), "failed to insert customer")
}
