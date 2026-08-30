package campaign

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListSuppressions — 会社のサプレッションリスト (検索 + ページング)。
func (s *CampaignService) ListSuppressions(
	ctx context.Context,
	req *connect.Request[campaignv1.ListSuppressionsRequest],
) (*connect.Response[campaignv1.ListSuppressionsResponse], error) {
	u, err := s.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	limit := int32(50)
	if req.Msg.Limit != nil {
		limit = *req.Msg.Limit
	}
	offset := int32(0)
	if req.Msg.Offset != nil {
		offset = *req.Msg.Offset
	}
	search := pgtype.Text{Valid: false}
	if req.Msg.Search != nil && *req.Msg.Search != "" {
		search = pgtype.Text{String: *req.Msg.Search, Valid: true}
	}
	rows, err := s.queries.ListSuppressionsByCompany(ctx, db.ListSuppressionsByCompanyParams{
		CompanyID: u.CompanyID, Search: search, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list suppressions: %w", err))
	}
	total, err := s.queries.CountSuppressionsByCompany(ctx, db.CountSuppressionsByCompanyParams{
		CompanyID: u.CompanyID, Search: search,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("count suppressions: %w", err))
	}
	protos := make([]*campaignv1.Suppression, 0, len(rows))
	for _, r := range rows {
		protos = append(protos, suppressionToProto(r))
	}
	return connect.NewResponse(&campaignv1.ListSuppressionsResponse{
		Suppressions: protos,
		Total:        total,
	}), nil
}

// AddSuppression — 手動追加 (reason=manual)。
func (s *CampaignService) AddSuppression(
	ctx context.Context,
	req *connect.Request[campaignv1.AddSuppressionRequest],
) (*connect.Response[campaignv1.AddSuppressionResponse], error) {
	u, err := s.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	email := normalizeEmail(req.Msg.Email)
	if email == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid email"))
	}
	if err := s.queries.CreateSuppression(ctx, db.CreateSuppressionParams{
		ID:         uuid.New(),
		CompanyID:  u.CompanyID,
		Lower:      email,
		Reason:     "manual",
		CampaignID: pgtype.UUID{Valid: false},
		Note:       req.Msg.Note,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create suppression: %w", err))
	}
	// ON CONFLICT DO NOTHING の場合も既存行を返す。
	sp, err := s.queries.GetSuppressionByEmail(ctx, db.GetSuppressionByEmailParams{
		CompanyID: u.CompanyID, Lower: email,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get suppression: %w", err))
	}
	return connect.NewResponse(&campaignv1.AddSuppressionResponse{
		Suppression: suppressionToProto(sp),
	}), nil
}

// RemoveSuppression — reason=manual の行のみ削除可。
// unsubscribe (法令) / hard_bounce (レピュテーション保護) は解除させない。
func (s *CampaignService) RemoveSuppression(
	ctx context.Context,
	req *connect.Request[campaignv1.RemoveSuppressionRequest],
) (*connect.Response[campaignv1.RemoveSuppressionResponse], error) {
	u, err := s.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid id: %w", err))
	}
	sp, err := s.queries.GetSuppression(ctx, id)
	if err != nil || sp.CompanyID != u.CompanyID {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("suppression not found"))
	}
	if sp.Reason != "manual" {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("理由 %s のサプレッションは解除できません (配信停止・バウンス由来の除外は保護されます)", sp.Reason))
	}
	if err := s.queries.DeleteSuppression(ctx, id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete suppression: %w", err))
	}
	return connect.NewResponse(&campaignv1.RemoveSuppressionResponse{Id: id.String()}), nil
}
