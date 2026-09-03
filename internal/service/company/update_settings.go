package company

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	companyv1 "github.com/0utl1er-tech/phox-customer/gen/pb/company/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/notify"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
)

// UpdateSettings は会社設定を更新する。User.role = owner のみ。
// Phase 27h から部分更新: call_log_mode は空文字、notify_* は未指定 (nil) が
// 「変更しない」を意味する。
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
			errors.New("会社設定の変更にはオーナー権限が必要です"))
	}

	// 通話記録モード (Phase 27f)。空文字は変更なし。
	if mode := req.Msg.CallLogMode; mode != "" {
		if mode != "click" && mode != "zoom" {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("call_log_mode は 'click' か 'zoom' を指定してください (got %q)", mode))
		}
		if _, err := s.queries.UpdateCompanyCallLogMode(ctx, db.UpdateCompanyCallLogModeParams{
			ID:          u.CompanyID,
			CallLogMode: mode,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update call_log_mode: %w", err))
		}
		log.Info().
			Str("company_id", u.CompanyID.String()).
			Str("call_log_mode", mode).
			Str("updated_by", u.ID).
			Msg("company settings: call_log_mode updated")
	}

	// 反響通知 (Phase 27h)。nil は変更なし。
	if req.Msg.NotifyWebhookUrl != nil || req.Msg.NotifyEvents != nil {
		params := db.UpdateCompanyNotifySettingsParams{ID: u.CompanyID}
		if req.Msg.NotifyWebhookUrl != nil {
			raw := req.Msg.GetNotifyWebhookUrl()
			if err := notify.ValidateWebhookURL(raw); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			params.NotifyWebhookUrl = pgtype.Text{String: raw, Valid: true}
		}
		if req.Msg.NotifyEvents != nil {
			normalized, err := notify.NormalizeEvents(req.Msg.GetNotifyEvents())
			if err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			params.NotifyEvents = pgtype.Text{String: normalized, Valid: true}
		}
		if _, err := s.queries.UpdateCompanyNotifySettings(ctx, params); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update notify settings: %w", err))
		}
		log.Info().
			Str("company_id", u.CompanyID.String()).
			Bool("webhook_set", req.Msg.GetNotifyWebhookUrl() != "").
			Str("notify_events", req.Msg.GetNotifyEvents()).
			Str("updated_by", u.ID).
			Msg("company settings: notify settings updated")
	}

	row, err := s.queries.GetCompanySettings(ctx, u.CompanyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get company settings: %w", err))
	}
	return connect.NewResponse(&companyv1.UpdateSettingsResponse{
		CallLogMode:      row.CallLogMode,
		NotifyWebhookUrl: row.NotifyWebhookUrl,
		NotifyEvents:     row.NotifyEvents,
	}), nil
}
