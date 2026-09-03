package customer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	"github.com/0utl1er-tech/phox-customer/internal/gemini"
	"github.com/0utl1er-tech/phox-customer/internal/service/customer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GEMINI_API_KEY 未設定時は FailedPrecondition を返すこと (DB 不要)。
func TestEnrichCustomerRows_DisabledWithoutKey(t *testing.T) {
	svc := customer.NewCustomerService(nil, nil, nil)

	_, err := svc.EnrichCustomerRows(context.Background(), connect.NewRequest(&customerv1.EnrichCustomerRowsRequest{
		Headers: []string{"会社名"},
		Rows:    []*customerv1.CustomerRowValues{{Cells: []string{"㈱テスト"}}},
	}))

	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

func TestGetEnrichmentStatus_Disabled(t *testing.T) {
	svc := customer.NewCustomerService(nil, nil, nil)

	resp, err := svc.GetEnrichmentStatus(context.Background(),
		connect.NewRequest(&customerv1.GetEnrichmentStatusRequest{}))

	require.NoError(t, err)
	assert.False(t, resp.Msg.Enabled)
	assert.Empty(t, resp.Msg.Model)
}

// モック Gemini で正常系: changed フラグの算出 (canonical マッピング比較) を検証。
func TestEnrichCustomerRows_ChangedFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// row 0: name を変更する提案 / row 1: 元の値と同一の提案 (changed=false)
		out := `[
			{"i": 0, "name": "田中 太郎", "confidence": 0.9},
			{"i": 1, "name": "既存 一致", "confidence": 0.8}
		]`
		resp := map[string]any{
			"candidates": []any{map[string]any{
				"content":      map[string]any{"parts": []any{map[string]any{"text": out}}},
				"finishReason": "STOP",
			}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	gem := gemini.NewClient("test-key", "test-model", gemini.WithAPIBase(srv.URL))
	svc := customer.NewCustomerService(nil, nil, gem)

	resp, err := svc.EnrichCustomerRows(context.Background(), connect.NewRequest(&customerv1.EnrichCustomerRowsRequest{
		Headers: []string{"name"},
		Rows: []*customerv1.CustomerRowValues{
			{Cells: []string{"たなかたろう"}},
			{Cells: []string{"既存 一致"}},
		},
	}))

	require.NoError(t, err)
	require.Len(t, resp.Msg.Rows, 2)
	assert.Equal(t, "test-model", resp.Msg.Model)

	byIdx := map[int32]*customerv1.EnrichedCustomerRow{}
	for _, r := range resp.Msg.Rows {
		byIdx[r.RowIndex] = r
	}
	assert.True(t, byIdx[0].Changed, "row 0 should be marked changed")
	assert.Equal(t, "田中 太郎", byIdx[0].Fields["name"])
	assert.False(t, byIdx[1].Changed, "row 1 proposal equals original → changed=false")
}

// 行数上限とバリデーション。
func TestEnrichCustomerRows_Validation(t *testing.T) {
	gem := gemini.NewClient("k", "m") // 実 API には到達しない (validation で弾かれる)
	svc := customer.NewCustomerService(nil, nil, gem)
	ctx := context.Background()

	// headers なし
	_, err := svc.EnrichCustomerRows(ctx, connect.NewRequest(&customerv1.EnrichCustomerRowsRequest{
		Rows: []*customerv1.CustomerRowValues{{Cells: []string{"x"}}},
	}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// rows なし
	_, err = svc.EnrichCustomerRows(ctx, connect.NewRequest(&customerv1.EnrichCustomerRowsRequest{
		Headers: []string{"a"},
	}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// 501 行 (上限 500 超)
	rows := make([]*customerv1.CustomerRowValues, 501)
	for i := range rows {
		rows[i] = &customerv1.CustomerRowValues{Cells: []string{"x"}}
	}
	_, err = svc.EnrichCustomerRows(ctx, connect.NewRequest(&customerv1.EnrichCustomerRowsRequest{
		Headers: []string{"a"},
		Rows:    rows,
	}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// 不正な target_fields
	_, err = svc.EnrichCustomerRows(ctx, connect.NewRequest(&customerv1.EnrichCustomerRowsRequest{
		Headers:      []string{"a"},
		Rows:         []*customerv1.CustomerRowValues{{Cells: []string{"x"}}},
		TargetFields: []string{"evil_field"},
	}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
