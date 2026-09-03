package company

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	companyv1 "github.com/0utl1er-tech/phox-customer/gen/pb/company/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/rs/zerolog/log"
)

// UpdateSettings は通話記録モードを更新する。User.role = owner のみ。
func (s *CompanyService) UpdateSettings(
	ctx context.Context,
	req *connect.Request[companyv1.UpdateSettingsRequest],
) (*connect.Response[companyv1.UpdateSettingsResponse], error) {
	u, err := s.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if u.Role != db.RoleOwner {
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("通話記録モードの変更にはオーナー権限が必要です"))
	}

	mode := req.Msg.CallLogMode
	if mode != "click" && mode != "zoom" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("call_log_mode は 'click' か 'zoom' を指定してください (got %q)", mode))
	}

	row, err := s.queries.UpdateCompanyCallLogMode(ctx, db.UpdateCompanyCallLogModeParams{
		ID:          u.CompanyID,
		CallLogMode: mode,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update call_log_mode: %w", err))
	}

	log.Info().
		Str("company_id", u.CompanyID.String()).
		Str("call_log_mode", row.CallLogMode).
		Str("updated_by", u.ID).
		Msg("company settings: call_log_mode updated")

	return connect.NewResponse(&companyv1.UpdateSettingsResponse{
		CallLogMode: row.CallLogMode,
	}), nil
}
