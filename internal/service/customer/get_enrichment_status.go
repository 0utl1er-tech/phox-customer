package customer

import (
	"context"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
)

// GetEnrichmentStatus は AI 補完 (Phase 27j) が利用可能かを返す。
// UI は CSV インポートダイアログを開いたときにこれを呼び、
// enabled=false なら「AIで補完・整形」ボタンを disabled にする。
func (s *CustomerService) GetEnrichmentStatus(
	ctx context.Context,
	req *connect.Request[customerv1.GetEnrichmentStatusRequest],
) (*connect.Response[customerv1.GetEnrichmentStatusResponse], error) {
	return connect.NewResponse(&customerv1.GetEnrichmentStatusResponse{
		Enabled: s.gemini.Enabled(),
		Model:   s.gemini.Model(),
	}), nil
}
