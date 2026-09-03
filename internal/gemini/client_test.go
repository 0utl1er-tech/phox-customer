package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient_DisabledWithoutKey(t *testing.T) {
	c := NewClient("", "whatever")
	if c != nil {
		t.Fatalf("expected nil client without api key")
	}
	if c.Enabled() { // nil-safe であること
		t.Fatalf("nil client must report Enabled() == false")
	}
	if c.Model() != "" {
		t.Fatalf("nil client must report empty model")
	}
}

func TestNewClient_DefaultModel(t *testing.T) {
	c := NewClient("key", "")
	if got := c.Model(); got != DefaultModel {
		t.Fatalf("default model = %q, want %q", got, DefaultModel)
	}
}

// fakeGemini は structured output を返すモック。入力の各行に対し
// name を "正規化済み" に置き換えた結果を返す。
func fakeGemini(t *testing.T, gotBodies *[]map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		if r.Header.Get("x-goog-api-key") == "" {
			http.Error(w, "no api key", http.StatusUnauthorized)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		*gotBodies = append(*gotBodies, body)

		// user text から入力行の "i" を抜き出す (雑だが十分)
		contents := body["contents"].([]any)
		parts := contents[0].(map[string]any)["parts"].([]any)
		text := parts[0].(map[string]any)["text"].(string)
		var rows []struct {
			I int `json:"i"`
		}
		idx := strings.Index(text, "[{")
		if idx >= 0 {
			_ = json.Unmarshal([]byte(text[idx:]), &rows)
		}

		outs := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			outs = append(outs, map[string]any{
				"i": r.I, "name": "正規化済み", "phone": "03-1234-5678", "confidence": 0.9,
			})
		}
		outJSON, _ := json.Marshal(outs)
		resp := map[string]any{
			"candidates": []any{
				map[string]any{
					"content": map[string]any{
						"parts": []any{map[string]any{"text": string(outJSON)}},
					},
					"finishReason": "STOP",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func TestNormalizeRows_BatchingAndParsing(t *testing.T) {
	var gotBodies []map[string]any
	srv := httptest.NewServer(fakeGemini(t, &gotBodies))
	defer srv.Close()

	c := NewClient("test-key", "test-model", WithAPIBase(srv.URL))

	// batchSize (20) を跨ぐ 45 行 → 3 リクエストに分割されること
	headers := []string{"会社", "担当", "TEL"}
	rows := make([][]string, 45)
	for i := range rows {
		rows[i] = []string{fmt.Sprintf("会社%d", i), "たなか たろう", "0312345678"}
	}

	results, err := c.NormalizeRows(context.Background(), headers, rows, nil)
	if err != nil {
		t.Fatalf("NormalizeRows: %v", err)
	}
	if len(gotBodies) != 3 {
		t.Fatalf("expected 3 batched requests, got %d", len(gotBodies))
	}
	if len(results) != 45 {
		t.Fatalf("expected 45 results, got %d", len(results))
	}
	// 添字がグローバル (バッチ跨ぎ) で保たれていること
	seen := map[int]bool{}
	for _, r := range results {
		seen[r.Index] = true
		if r.Fields["name"] != "正規化済み" {
			t.Fatalf("row %d: unexpected name %q", r.Index, r.Fields["name"])
		}
		if r.Confidence != 0.9 {
			t.Fatalf("row %d: unexpected confidence %v", r.Index, r.Confidence)
		}
	}
	for i := 0; i < 45; i++ {
		if !seen[i] {
			t.Fatalf("missing result for row %d", i)
		}
	}
}

func TestNormalizeRows_TargetFieldsFilter(t *testing.T) {
	var gotBodies []map[string]any
	srv := httptest.NewServer(fakeGemini(t, &gotBodies))
	defer srv.Close()

	c := NewClient("test-key", "test-model", WithAPIBase(srv.URL))
	results, err := c.NormalizeRows(context.Background(),
		[]string{"a"}, [][]string{{"x"}}, []string{"phone"})
	if err != nil {
		t.Fatalf("NormalizeRows: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// target_fields = ["phone"] なので name はフィルタされる
	if _, ok := results[0].Fields["name"]; ok {
		t.Fatalf("name should be filtered out by target_fields")
	}
	if results[0].Fields["phone"] != "03-1234-5678" {
		t.Fatalf("phone missing from result: %v", results[0].Fields)
	}
}

func TestNormalizeRows_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"blocked","status":"PERMISSION_DENIED"}}`))
	}))
	defer srv.Close()

	c := NewClient("bad-key", "m", WithAPIBase(srv.URL))
	_, err := c.NormalizeRows(context.Background(), []string{"a"}, [][]string{{"x"}}, nil)
	if err == nil {
		t.Fatalf("expected error on 403")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("error should carry API message, got: %v", err)
	}
}

func TestNormalizeRows_FabricatedIndexDropped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := `[{"i": 999, "name": "捏造", "confidence": 1.0}, {"i": 0, "name": "ok", "confidence": 1.0}]`
		resp := map[string]any{
			"candidates": []any{map[string]any{
				"content":      map[string]any{"parts": []any{map[string]any{"text": out}}},
				"finishReason": "STOP",
			}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient("k", "m", WithAPIBase(srv.URL))
	results, err := c.NormalizeRows(context.Background(), []string{"a"}, [][]string{{"x"}}, nil)
	if err != nil {
		t.Fatalf("NormalizeRows: %v", err)
	}
	if len(results) != 1 || results[0].Index != 0 {
		t.Fatalf("fabricated row index should be dropped, got %+v", results)
	}
}
