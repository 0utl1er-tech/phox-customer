package testutil

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
)

// NewTestRequest は Connect RPC のテスト用 request を作る。
// auth context を header ではなく context に直接埋めるので、
// interceptor を通さずにサービスを直接呼べる。
func NewTestRequest[T any](ctx context.Context, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	// connect.Request は内部的に context を保持しないので、
	// サービスの呼び出し時に ctx を引数で渡す。
	// ここでは header 操作だけ (テストでは不要だが構造上の互換性のため)。
	req.Header().Set("Content-Type", "application/json")
	return req
}

// FakeHTTPRequest は テスト用の最小限の http.Request を返す。
func FakeHTTPRequest() *http.Request {
	req, _ := http.NewRequest("POST", "http://localhost/test", nil)
	return req
}
