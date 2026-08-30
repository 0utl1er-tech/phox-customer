package campaign

import (
	"context"
	"time"

	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	campaignpkg "github.com/0utl1er-tech/phox-customer/internal/campaign"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// campaignToProto は Campaign 行を proto に変換する。stats / mailbox_ids は
// 追加クエリで引く (list でも件数上限 100 なので N+1 は許容)。
func (s *CampaignService) campaignToProto(ctx context.Context, c db.Campaign) (*campaignv1.Campaign, error) {
	stats, err := s.queries.GetCampaignStats(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	mailboxes, err := s.queries.ListCampaignMailboxes(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	mailboxIDs := make([]string, 0, len(mailboxes))
	for _, mb := range mailboxes {
		mailboxIDs = append(mailboxIDs, mb.ID.String())
	}
	p := &campaignv1.Campaign{
		Id:          c.ID.String(),
		Name:        c.Name,
		Status:      c.Status,
		Subject:     c.Subject,
		Body:        c.Body,
		TrackOpens:  c.TrackOpens,
		TrackClicks: c.TrackClicks,
		Schedule: &campaignv1.CampaignSchedule{
			SendStartHour:      c.SendStartHour,
			SendEndHour:        c.SendEndHour,
			SendDays:           c.SendDays,
			DailyCapPerMailbox: c.DailyCapPerMailbox,
			MinIntervalSec:     c.MinIntervalSec,
			WarmupEnabled:      c.WarmupEnabled,
		},
		Sender: &campaignv1.CampaignSender{
			SenderOrg:     c.SenderOrg,
			SenderAddress: c.SenderAddress,
			SenderContact: c.SenderContact,
		},
		MailboxIds: mailboxIDs,
		Stats: &campaignv1.CampaignStats{
			Total:        int32(stats.Total),
			Queued:       int32(stats.Queued),
			Sent:         int32(stats.Sent),
			Failed:       int32(stats.Failed),
			Skipped:      int32(stats.Skipped),
			Opened:       int32(stats.Opened),
			Clicked:      int32(stats.Clicked),
			Replied:      int32(stats.Replied),
			Bounced:      int32(stats.Bounced),
			Unsubscribed: int32(stats.Unsubscribed),
		},
		CreatedBy: c.CreatedBy,
		CreatedAt: timestamppb.New(c.CreatedAt),
		UpdatedAt: timestamppb.New(c.UpdatedAt),
	}
	if c.StartedAt.Valid {
		p.StartedAt = timestamppb.New(c.StartedAt.Time)
	}
	if c.CompletedAt.Valid {
		p.CompletedAt = timestamppb.New(c.CompletedAt.Time)
	}
	// 「今動いている」表示用の最終送信時刻 (max() は sqlc 上 interface{})。
	if t, ok := stats.LastSentAt.(time.Time); ok && !t.IsZero() {
		p.Stats.LastSentAt = timestamppb.New(t)
	}
	// running 中のみ完了予定時刻を見積もる (ペーシング設定のシミュレーション)。
	if c.Status == "running" && stats.Queued > 0 {
		now := time.Now()
		midnight := campaignpkg.JSTMidnight(now)
		var sentToday int64
		for _, mb := range mailboxes {
			n, cerr := s.queries.CountSentSinceByMailbox(ctx, db.CountSentSinceByMailboxParams{
				MailboxID: pgtype.UUID{Bytes: mb.ID, Valid: true},
				SentAt:    pgtype.Timestamptz{Time: midnight, Valid: true},
			})
			if cerr == nil {
				sentToday += n
			}
		}
		if eta := campaignpkg.EstimateCompletion(c, len(mailboxes), stats.Queued, sentToday, now); eta != nil {
			p.EstimatedCompletionAt = timestamppb.New(*eta)
		}
	}
	return p, nil
}

func recipientRowToProto(r db.ListCampaignRecipientsRow) *campaignv1.CampaignRecipient {
	p := &campaignv1.CampaignRecipient{
		Id:                  r.ID.String(),
		CustomerId:          r.CustomerID.String(),
		CustomerName:        r.CustomerName,
		CustomerCorporation: r.CustomerCorporation,
		Email:               r.Email,
		Status:              r.Status,
		Error:               r.Error,
	}
	setTS := func(dst **timestamppb.Timestamp, src pgtype.Timestamptz) {
		if src.Valid {
			*dst = timestamppb.New(src.Time)
		}
	}
	setTS(&p.SentAt, r.SentAt)
	setTS(&p.FirstOpenedAt, r.FirstOpenedAt)
	setTS(&p.FirstClickedAt, r.FirstClickedAt)
	setTS(&p.RepliedAt, r.RepliedAt)
	setTS(&p.BouncedAt, r.BouncedAt)
	setTS(&p.UnsubscribedAt, r.UnsubscribedAt)
	return p
}

func suppressionToProto(sp db.Suppression) *campaignv1.Suppression {
	p := &campaignv1.Suppression{
		Id:        sp.ID.String(),
		Email:     sp.Email,
		Reason:    sp.Reason,
		Note:      sp.Note,
		CreatedAt: timestamppb.New(sp.CreatedAt),
	}
	if sp.CampaignID.Valid {
		s := uuid.UUID(sp.CampaignID.Bytes).String()
		p.CampaignId = &s
	}
	return p
}
