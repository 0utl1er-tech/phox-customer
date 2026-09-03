package customer

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	"github.com/0utl1er-tech/phox-customer/internal/gemini"
	"github.com/rs/zerolog/log"
)

// enrichMaxRows は 1 リクエストで受け付ける最大行数 (コスト対策)。
// proto の buf.validate と二重だが、validate interceptor 未導入経路 (MCP 等)
// でも守られるようにサーバ側でもチェックする。
const enrichMaxRows = 500

// EnrichCustomerRows は CSV 取り込みプレビューの行データを Gemini で正規化・
// 補完した「提案」を返す (Phase 27j)。
//
//   - 元データは一切変更しない。DB にも書かない。UI がプレビューとして表示し、
//     ユーザーが適用した行だけが通常の ImportBook に渡る。
//   - GEMINI_API_KEY 未設定なら CodeFailedPrecondition。
//   - バッチ分割・行数上限などのコスト対策はサーバ側で行う。
func (s *CustomerService) EnrichCustomerRows(
	ctx context.Context,
	req *connect.Request[customerv1.EnrichCustomerRowsRequest],
) (*connect.Response[customerv1.EnrichCustomerRowsResponse], error) {
	if !s.gemini.Enabled() {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("AI 補完は無効です: 管理者が GEMINI_API_KEY を設定してください"))
	}

	headers := req.Msg.Headers
	if len(headers) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("headers is required"))
	}
	if len(req.Msg.Rows) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("rows is required"))
	}
	if len(req.Msg.Rows) > enrichMaxRows {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("rows must be at most %d per request", enrichMaxRows))
	}
	// target_fields は canonical 名のみ許可
	valid := make(map[string]bool, len(gemini.CanonicalFields))
	for _, f := range gemini.CanonicalFields {
		valid[f] = true
	}
	for _, f := range req.Msg.TargetFields {
		if !valid[f] {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("unknown target field: %q", f))
		}
	}

	rows := make([][]string, len(req.Msg.Rows))
	for i, r := range req.Msg.Rows {
		rows[i] = r.Cells
	}

	results, err := s.gemini.NormalizeRows(ctx, headers, rows, req.Msg.TargetFields)
	if err != nil {
		log.Error().Err(err).Int("rows", len(rows)).Msg("EnrichCustomerRows: gemini call failed")
		return nil, connect.NewError(connect.CodeUnavailable,
			fmt.Errorf("AI 補完に失敗しました: %v", err))
	}

	out := make([]*customerv1.EnrichedCustomerRow, 0, len(results))
	for _, r := range results {
		orig := mapRowToCanonical(headers, rows[r.Index])
		changed := false
		for k, v := range r.Fields {
			if strings.TrimSpace(orig[k]) != v {
				changed = true
				break
			}
		}
		out = append(out, &customerv1.EnrichedCustomerRow{
			RowIndex:   int32(r.Index),
			Fields:     r.Fields,
			Confidence: r.Confidence,
			Changed:    changed,
		})
	}

	return connect.NewResponse(&customerv1.EnrichCustomerRowsResponse{
		Rows:  out,
		Model: s.gemini.Model(),
	}), nil
}

// mapRowToCanonical は ImportBook と同じルールでヘッダを canonical フィールドに
// マッピングする (lower(trim), mail は email も可)。changed フラグの算出用。
func mapRowToCanonical(headers []string, row []string) map[string]string {
	out := map[string]string{}
	for i, h := range headers {
		if i >= len(row) {
			break
		}
		key := strings.ToLower(strings.TrimSpace(h))
		if key == "email" {
			key = "mail"
		}
		switch key {
		case "phone", "category", "name", "corporation", "address", "memo", "mail":
			out[key] = row[i]
		}
	}
	return out
}
