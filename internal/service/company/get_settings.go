package company

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	companyv1 "github.com/0utl1er-tech/phox-customer/gen/pb/company/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

// GetSettings は呼び出しユーザーの会社の設定を返す。閲覧は誰でも可。
// can_edit は User.role = owner かどうか (UI がフォームの活性を切り替える)。
func (s *CompanyService) GetSettings(
	ctx context.Context,
	req *connect.Request[companyv1.GetSettingsRequest],
) (*connect.Response[companyv1.GetSettingsResponse], error) {
	u, err := s.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	row, err := s.queries.GetCompanySettings(ctx, u.CompanyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get company settings: %w", err))
	}
	return connect.NewResponse(&companyv1.GetSettingsResponse{
		CallLogMode: row.CallLogMode,
		CanEdit:     u.Role == db.RoleOwner,
	}), nil
}
