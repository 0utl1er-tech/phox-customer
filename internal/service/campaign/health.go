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
