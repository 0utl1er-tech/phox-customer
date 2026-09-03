// Package company implements CompanyService — 会社単位の管理者設定 (Phase 27f)。
//
// 現状の設定項目は通話記録モード (call_log_mode) のみ:
//   - 'click' … 従来どおり。電話番号クリック時のフォールバック経路でも
//     コール活動を自動記録する (デフォルト)。
//   - 'zoom'  … Zoom 通話履歴をマスターにする。フォールバックでの自動記録を
//     廃止し、internal/zoom.ReconcileWorker が毎時 call_logs を同期する。
//
// RBAC:
//   - GetSettings は同じ会社のユーザーなら誰でも可 (can_edit フラグで
//     UI 側の活性状態を制御)。
//   - UpdateSettings は User.role = owner のみ。
package company

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

type CompanyService struct {
	queries *db.Queries
}

func NewCompanyService(queries *db.Queries) *CompanyService {
	return &CompanyService{queries: queries}
}

// currentUser は認証済みユーザーの User 行を返す (会社スコープの起点)。
func (s *CompanyService) currentUser(ctx context.Context) (db.User, error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return db.User{}, err
	}
	u, err := s.queries.GetUser(ctx, token.Subject())
	if err != nil {
		return db.User{}, connect.NewError(connect.CodePermissionDenied, errors.New("user not found"))
	}
	return u, nil
}
