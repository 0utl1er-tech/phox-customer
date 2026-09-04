package book_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/customfields"
	"github.com/0utl1er-tech/phox-customer/internal/service/book"
)

// fakeDBTX は sqlc の DBTX を満たす最小 fake。ImportBook が使うクエリは
// すべて QueryRow → Scan なので、Scan が no-op の row を返せば動く。
// Customer INSERT だけは ID を記録し、同一 ID の二回目で重複キーエラーを
// 返して挿入段階の失敗を再現する。
type fakeDBTX struct {
	insertedCustomerIDs map[uuid.UUID]bool
	// insertedCustomerArgs は Customer INSERT の実引数 (sqlc の列順) を
	// 挿入順で残す。Phase 29b の custom_fields 検証に使う。
	insertedCustomerArgs [][]any
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
		f.insertedCustomerArgs = append(f.insertedCustomerArgs, args)
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

// insertedCustomer は Customer INSERT 引数を読みやすい形にほどく
// (sqlc の $1..$10 = id, book_id, phone, category, name, corporation,
// address, memo, mail, custom_fields)。
type insertedCustomer struct {
	name         string
	phone        string
	mail         string
	memo         string
	customFields map[string]string
}

func (f *fakeDBTX) customers(t *testing.T) []insertedCustomer {
	t.Helper()
	out := make([]insertedCustomer, 0, len(f.insertedCustomerArgs))
	for _, a := range f.insertedCustomerArgs {
		require.Len(t, a, 10)
		cf := map[string]string{}
		require.NoError(t, json.Unmarshal(a[9].([]byte), &cf))
		out = append(out, insertedCustomer{
			name: a[4].(string), phone: a[2].(string), mail: a[8].(string),
			memo: a[7].(string), customFields: cf,
		})
	}
	return out
}

func importCSV(t *testing.T, csv string) (*fakeDBTX, *bookv1.ImportBookResponse) {
	t.Helper()
	fake := &fakeDBTX{insertedCustomerIDs: map[uuid.UUID]bool{}}
	svc := book.NewBookService(db.New(fake), nil)
	resp, err := svc.ImportBook(context.Background(), connect.NewRequest(&bookv1.ImportBookRequest{
		FileName:    "custom-fields-test.csv",
		FileContent: []byte(csv),
		OwnerId:     "import-book-test-owner",
	}))
	require.NoError(t, err)
	return fake, resp.Msg
}

// Phase 29b: canonical 以外の列は custom_fields に入り、canonical 列の扱いは
// 従来どおり (未知列を足しても name/phone/mail/memo は変わらない)。
func TestImportBook_UnknownColumnsBecomeCustomFields(t *testing.T) {
	fake, msg := importCSV(t, "name,mail,phone,memo,meo_score,meo_issues\n"+
		"みどり整体院,midori@example.com,03-1111-2222,既存メモ,30/100,\"・写真が3枚以下\n・投稿が90日以上停止\"\n")

	assert.Equal(t, int32(1), msg.GetImportedCount())
	got := fake.customers(t)
	require.Len(t, got, 1)

	// canonical 列の挙動は不変
	assert.Equal(t, "みどり整体院", got[0].name)
	assert.Equal(t, "midori@example.com", got[0].mail)
	assert.Equal(t, "03-1111-2222", got[0].phone)
	assert.Equal(t, "既存メモ", got[0].memo)

	// 未知列は差し込み変数に。改行を含むセルはそのまま保持される
	// (箇条書きの診断結果が主用途なので改行を潰してはいけない)。
	assert.Equal(t, map[string]string{
		"meo_score":  "30/100",
		"meo_issues": "・写真が3枚以下\n・投稿が90日以上停止",
	}, got[0].customFields)
}

// canonical 列だけの CSV は custom_fields が空 (既存挙動の回帰)。
// email 別名や大文字ヘッダも canonical として吸われ、変数化しない。
func TestImportBook_CanonicalOnlyLeavesCustomFieldsEmpty(t *testing.T) {
	fake, _ := importCSV(t, "Name,EMAIL,Corporation,Address,Category,Memo,Phone\n"+
		"山田,yamada@example.com,株式会社サンプル,東京都,整体,メモ,090-0000-0000\n")

	got := fake.customers(t)
	require.Len(t, got, 1)
	assert.Equal(t, "山田", got[0].name)
	assert.Equal(t, "yamada@example.com", got[0].mail)
	assert.Empty(t, got[0].customFields)
}

// 列名の正規化: 大文字→小文字、[a-z0-9_] 以外は '_'。値が空のセルは
// キーごと落とす。日本語だけの列名はキーにならないので従来どおり無視。
func TestImportBook_CustomFieldKeyNormalisation(t *testing.T) {
	fake, _ := importCSV(t, "name,MEO Score,web-site,店舗メモ,empty_col\n"+
		"テスト,42,https://example.com,無視される,\n")

	got := fake.customers(t)
	require.Len(t, got, 1)
	assert.Equal(t, map[string]string{
		"meo_score": "42",
		"web_site":  "https://example.com",
	}, got[0].customFields)
}

// 防御: 列数は MaxFields まで、値は MaxValueLen バイトまで。
func TestImportBook_CustomFieldLimits(t *testing.T) {
	var header, row strings.Builder
	header.WriteString("name")
	row.WriteString("テスト")
	for i := 0; i < customfields.MaxFields+10; i++ {
		fmt.Fprintf(&header, ",col%02d", i)
		row.WriteString(",v")
	}
	// 上限超えの値 (4KB + はみ出し)
	header.WriteString(",big")
	row.WriteString("," + strings.Repeat("あ", customfields.MaxValueLen))

	fake, _ := importCSV(t, header.String()+"\n"+row.String()+"\n")
	got := fake.customers(t)
	require.Len(t, got, 1)

	assert.Len(t, got[0].customFields, customfields.MaxFields,
		"列数は MaxFields で頭打ちになる")
	// 先頭 32 列が採用され、あふれた列 (big 含む) は入らない。
	assert.Contains(t, got[0].customFields, "col00")
	assert.NotContains(t, got[0].customFields, "big")

	// 値の切り詰め単体 (列数上限に邪魔されない形で確認)。
	fake2, _ := importCSV(t, "name,big\nテスト,"+strings.Repeat("あ", customfields.MaxValueLen)+"\n")
	got2 := fake2.customers(t)
	require.Len(t, got2, 1)
	assert.LessOrEqual(t, len(got2[0].customFields["big"]), customfields.MaxValueLen)
	assert.True(t, utf8.ValidString(got2[0].customFields["big"]),
		"切り詰めても UTF-8 として壊れない")
}
