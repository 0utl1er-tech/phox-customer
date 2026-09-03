package campaign

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/rs/zerolog/log"
)

// StartCampaign — draft/paused から running へ。
// 特定電子メール法の送信者表示 (sender_org / sender_address / sender_contact)
// が空のままの開始は拒否する — フッター表示は省略できない法定事項のため。
func (s *CampaignService) StartCampaign(
	ctx context.Context,
	req *connect.Request[campaignv1.StartCampaignRequest],
) (*connect.Response[campaignv1.StartCampaignResponse], error) {
	c, _, err := s.getCampaignScoped(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if err := s.checkPoolPermission(ctx, c.ID); err != nil {
		return nil, err
	}
	if c.Status != "draft" && c.Status != "paused" {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("status %s のキャンペーンは開始できません", c.Status))
	}
	if c.SenderOrg == "" || c.SenderAddress == "" || c.SenderContact == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("特定電子メール法の送信者表示 (会社名・住所・連絡先) が未入力です"))
	}
	if c.Subject == "" || c.Body == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("件名と本文を入力してください"))
	}
	updated, err := s.queries.SetCampaignStatus(ctx, db.SetCampaignStatusParams{ID: c.ID, Status: "running"})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("start campaign: %w", err))
	}
	// Phase 27f: 人が原因を確認して再開したので、自動停止の理由は消す。
	// (しきい値を超えたままならサーキットブレーカーが再び止める。)
	if c.HealthPausedReason != "" {
		if cerr := s.queries.ClearHealthPauseReason(ctx, c.ID); cerr != nil {
			log.Warn().Err(cerr).Str("campaign", c.ID.String()).Msg("campaign: clear health pause reason failed")
		}
		updated.HealthPausedReason = ""
	}
	p, err := s.campaignToProto(ctx, updated)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&campaignv1.StartCampaignResponse{Campaign: p}), nil
}

// PauseCampaign — running -> paused。
func (s *CampaignService) PauseCampaign(
	ctx context.Context,
	req *connect.Request[campaignv1.PauseCampaignRequest],
) (*connect.Response[campaignv1.PauseCampaignResponse], error) {
	c, _, err := s.getCampaignScoped(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if err := s.checkPoolPermission(ctx, c.ID); err != nil {
		return nil, err
	}
	if c.Status != "running" {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("status %s のキャンペーンは一時停止できません", c.Status))
	}
	updated, err := s.queries.SetCampaignStatus(ctx, db.SetCampaignStatusParams{ID: c.ID, Status: "paused"})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("pause campaign: %w", err))
	}
	p, err := s.campaignToProto(ctx, updated)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&campaignv1.PauseCampaignResponse{Campaign: p}), nil
}

// CancelCampaign — draft/running/paused -> cancelled (終了状態、再開不可)。
func (s *CampaignService) CancelCampaign(
	ctx context.Context,
	req *connect.Request[campaignv1.CancelCampaignRequest],
) (*connect.Response[campaignv1.CancelCampaignResponse], error) {
	c, _, err := s.getCampaignScoped(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if err := s.checkPoolPermission(ctx, c.ID); err != nil {
		return nil, err
	}
	if c.Status == "completed" || c.Status == "cancelled" {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("status %s のキャンペーンは中止できません", c.Status))
	}
	updated, err := s.queries.SetCampaignStatus(ctx, db.SetCampaignStatusParams{ID: c.ID, Status: "cancelled"})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("cancel campaign: %w", err))
	}
	p, err := s.campaignToProto(ctx, updated)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&campaignv1.CancelCampaignResponse{Campaign: p}), nil
}

// DeleteCampaign — draft/cancelled のみ (履歴のあるものは消さない)。
func (s *CampaignService) DeleteCampaign(
	ctx context.Context,
	req *connect.Request[campaignv1.DeleteCampaignRequest],
) (*connect.Response[campaignv1.DeleteCampaignResponse], error) {
	c, _, err := s.getCampaignScoped(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if err := s.checkPoolPermission(ctx, c.ID); err != nil {
		return nil, err
	}
	if c.Status != "draft" && c.Status != "cancelled" {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("status %s のキャンペーンは削除できません (中止してから削除してください)", c.Status))
	}
	if err := s.queries.DeleteCampaign(ctx, c.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete campaign: %w", err))
	}
	return connect.NewResponse(&campaignv1.DeleteCampaignResponse{Id: c.ID.String()}), nil
}
