package campaign

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	campaignpkg "github.com/0utl1er-tech/phox-customer/internal/campaign"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// healthWindow は健全性判定に使う実績の期間。
	healthWindow = 30 * 24 * time.Hour
	// バウンス率の判定しきい値 (%)。業界的に 2% 超で要注意、5% 超で危険。
	bounceWarnRate = 2.0
	bounceBadRate  = 5.0
	// 配信停止率の判定しきい値 (%)。
	unsubWarnRate = 3.0
	// これ未満の送信数では率を判定材料にしない。
	minSamplesForRate = 20
)

// CheckMailboxHealth は送信元メールボックスの健全性を点検する (Phase 27f)。
//
// バウンス率が高いまま送り続けると送信ドメインの評価が落ち、以後の全メールが
// スパム判定される。DNS 設定 (SPF/DKIM/DMARC) の不備も同じ結果を招くため、
// 「送る前に確認すべきこと」をまとめて 1 画面で返す。
func (s *CampaignService) CheckMailboxHealth(
	ctx context.Context,
	req *connect.Request[campaignv1.CheckMailboxHealthRequest],
) (*connect.Response[campaignv1.CheckMailboxHealthResponse], error) {
	u, err := s.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	mbID, err := uuid.Parse(req.Msg.MailboxId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid mailbox_id: %w", err))
	}
	// 閲覧なので viewer 以上で足りる。
	if err := s.authorizer.CheckMailboxPermission(ctx, mbID, db.RoleViewer); err != nil {
		return nil, err
	}
	mb, err := s.queries.GetMailbox(ctx, mbID)
	if err != nil || mb.CompanyID != u.CompanyID {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("mailbox not found"))
	}

	selector := req.Msg.DkimSelector
	if selector == "" {
		selector = "dkim" // mailu の既定
	}
	domain := campaignpkg.DomainOf(mb.Address)
	dns := campaignpkg.CheckSenderDomain(ctx, domain, selector)

	st, err := s.queries.GetMailboxBounceStats(ctx, db.GetMailboxBounceStatsParams{
		MailboxID: pgtype.UUID{Bytes: mbID, Valid: true},
		SentAt:    pgtype.Timestamptz{Time: time.Now().Add(-healthWindow), Valid: true},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("mailbox bounce stats: %w", err))
	}

	h := &campaignv1.MailboxHealth{
		MailboxId:    mbID.String(),
		Address:      mb.Address,
		Domain:       domain,
		HasMx:        dns.HasMX,
		MxHost:       dns.MXHost,
		HasSpf:       dns.HasSPF,
		Spf:          dns.SPF,
		HasDmarc:     dns.HasDMARC,
		Dmarc:        dns.DMARC,
		DmarcPolicy:  dns.DMARCPolicy,
		HasDkim:      dns.HasDKIM,
		DkimSelector: selector,
		Sent:         int32(st.Sent),
		Bounced:      int32(st.Bounced),
		Unsubscribed: int32(st.Unsubscribed),
		Replied:      int32(st.Replied),
		Warnings:     dns.Warnings,
	}
	if st.Sent > 0 {
		h.BounceRate = float64(st.Bounced) * 100 / float64(st.Sent)
		h.UnsubscribeRate = float64(st.Unsubscribed) * 100 / float64(st.Sent)
	}

	// 実績ベースの警告 (サンプルが少ないうちは率で騒がない)。
	if st.Sent >= minSamplesForRate {
		if h.BounceRate > bounceBadRate {
			h.Warnings = append(h.Warnings, fmt.Sprintf(
				"バウンス率が %.1f%% と高すぎます (%.0f%% 超は危険域)。宛先リストの品質を見直してください",
				h.BounceRate, bounceBadRate))
		} else if h.BounceRate > bounceWarnRate {
			h.Warnings = append(h.Warnings, fmt.Sprintf(
				"バウンス率が %.1f%% です (%.0f%% 超で要注意)。リストの鮮度を確認してください",
				h.BounceRate, bounceWarnRate))
		}
		if h.UnsubscribeRate > unsubWarnRate {
			h.Warnings = append(h.Warnings, fmt.Sprintf(
				"配信停止率が %.1f%% です。文面やターゲットが受け手に合っていない可能性があります",
				h.UnsubscribeRate))
		}
	}

	h.Grade = grade(dns, h, st.Sent)
	return connect.NewResponse(&campaignv1.CheckMailboxHealthResponse{Health: h}), nil
}

// ListMailboxesHealth は呼び出しユーザーが MailboxPermit を持つ全メールボックスの
// 送信実績サマリを 1 コールで返す (Phase 27g)。
//
// mailbox 管理画面が一覧表示のたびに呼ぶため DB 集計のみで軽く済ませる。
// DNS 点検 (SPF/DKIM/DMARC) は重いので、ここには含めず CheckMailboxHealth を
// オンデマンド (詳細チェックボタン) で呼んでもらう。
// RBAC: permit join で ListMailboxesByUserID の対象と揃う (viewer で可 — 閲覧のみ)。
func (s *CampaignService) ListMailboxesHealth(
	ctx context.Context,
	req *connect.Request[campaignv1.ListMailboxesHealthRequest],
) (*connect.Response[campaignv1.ListMailboxesHealthResponse], error) {
	u, err := s.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	rows, err := s.queries.ListMailboxHealthStatsByUserID(ctx, db.ListMailboxHealthStatsByUserIDParams{
		UserID:      u.ID,
		TodayStart:  pgtype.Timestamptz{Time: campaignpkg.JSTMidnight(now), Valid: true},
		WindowStart: pgtype.Timestamptz{Time: now.Add(-healthWindow), Valid: true},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("mailbox health stats: %w", err))
	}

	stats := make([]*campaignv1.MailboxHealthStats, 0, len(rows))
	for _, r := range rows {
		st := &campaignv1.MailboxHealthStats{
			MailboxId:        r.ID.String(),
			Address:          r.Address,
			SentToday:        int32(r.SentToday),
			Sent_30D:         int32(r.Sent30d),
			Bounced_30D:      int32(r.Bounced30d),
			Unsubscribed_30D: int32(r.Unsubscribed30d),
			Replied_30D:      int32(r.Replied30d),
			Opened_30D:       int32(r.Opened30d),
			RunningCampaigns: int32(r.RunningCampaigns),
		}
		if t, ok := r.LastSentAt.(time.Time); ok && !t.IsZero() {
			st.LastSentAt = timestamppb.New(t)
		}
		if r.SyncedAt.Valid {
			st.ImapSyncedAt = timestamppb.New(r.SyncedAt.Time)
		}
		if r.Sent30d > 0 {
			st.BounceRate = float64(r.Bounced30d) * 100 / float64(r.Sent30d)
			st.UnsubscribeRate = float64(r.Unsubscribed30d) * 100 / float64(r.Sent30d)
			st.ReplyRate = float64(r.Replied30d) * 100 / float64(r.Sent30d)
			st.OpenRate = float64(r.Opened30d) * 100 / float64(r.Sent30d)
		}
		st.Grade = statsGrade(r.Sent30d, st.BounceRate, st.UnsubscribeRate)
		stats = append(stats, st)
	}
	return connect.NewResponse(&campaignv1.ListMailboxesHealthResponse{Stats: stats}), nil
}

// statsGrade は実績のみの簡易判定 (DNS を見ないので CheckMailboxHealth の
// grade より甘い)。しきい値は grade と同一。サンプルが少ないうちは率で騒がない。
func statsGrade(sent int64, bounceRate, unsubRate float64) string {
	if sent < minSamplesForRate {
		return "good"
	}
	if bounceRate > bounceBadRate {
		return "bad"
	}
	if bounceRate > bounceWarnRate || unsubRate > unsubWarnRate {
		return "warn"
	}
	return "good"
}

// grade は総合判定。DNS の必須項目 (SPF/DMARC) 欠落と高バウンス率を bad、
// それ以外の指摘があれば warn とする。
func grade(dns campaignpkg.SenderDomainHealth, h *campaignv1.MailboxHealth, sent int64) string {
	if !dns.HasSPF || !dns.HasDMARC || !dns.HasMX {
		return "bad"
	}
	if sent >= minSamplesForRate && h.BounceRate > bounceBadRate {
		return "bad"
	}
	if len(h.Warnings) > 0 {
		return "warn"
	}
	return "good"
}
