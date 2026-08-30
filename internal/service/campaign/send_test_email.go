package campaign

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	campaignpkg "github.com/0utl1er-tech/phox-customer/internal/campaign"
	"github.com/google/uuid"
	"strings"
	"time"
)

// SendTestEmail — 実受信者と同じレンダリング (プレースホルダはサンプル値、
// 特電法フッター込み) を指定アドレスへ 1 通送る。DB には何も記録しない。
// 配信停止 URL は形式だけ本物のダミー (uuid.Nil ベースなので踏んでも 404)。
func (s *CampaignService) SendTestEmail(
	ctx context.Context,
	req *connect.Request[campaignv1.SendTestEmailRequest],
) (*connect.Response[campaignv1.SendTestEmailResponse], error) {
	if s.sender == nil || s.cipher == nil {
		return nil, connect.NewError(connect.CodeUnavailable,
			errors.New("mailbox sending is not configured (MAILU_SMTP_* / MAILBOX_SECRET_KEY)"))
	}
	c, u, err := s.getCampaignScoped(ctx, req.Msg.CampaignId)
	if err != nil {
		return nil, err
	}
	mbID, err := uuid.Parse(req.Msg.MailboxId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid mailbox_id: %w", err))
	}
	if err := s.authorizer.CheckMailboxPermission(ctx, mbID, db.RoleEditor); err != nil {
		return nil, err
	}
	mb, err := s.queries.GetMailbox(ctx, mbID)
	if err != nil || mb.CompanyID != u.CompanyID {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("mailbox not found"))
	}
	if !mb.Active {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("このメールボックスは無効化されています"))
	}
	password, err := s.cipher.DecryptString(mb.PasswordEnc)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("decrypt mailbox password: %w", err))
	}

	unsubURL := "(実送信時に配信停止URLが入ります)"
	if s.tokenizer != nil && s.publicBase != "" {
		unsubURL = strings.TrimSuffix(s.publicBase, "/") + "/u/" + s.tokenizer.Token(campaignpkg.KindUnsubscribe, uuid.Nil, 0)
	}
	vars := map[string]string{
		"customer_name":        "(テスト) 山田太郎",
		"customer_corporation": "(テスト) 株式会社サンプル",
		"customer_mail":        req.Msg.To,
		"customer_phone":       "03-0000-0000",
		"sender_name":          mb.DisplayName,
		"sender_mail":          mb.Address,
		"today":                campaignpkg.TodayJST(time.Now()),
	}
	subject := "[テスト] " + campaignpkg.Render(c.Subject, vars)
	body := campaignpkg.RenderBody(c.Body, vars, campaignpkg.SenderInfo{
		Org: c.SenderOrg, Address: c.SenderAddress, Contact: c.SenderContact,
	}, unsubURL)

	if err := s.sender.SendAs(ctx,
		mb.SmtpUsername, password,
		mb.Address, mb.DisplayName,
		req.Msg.To, nil,
		subject, body, ""); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("smtp send (test): %w", err))
	}
	return connect.NewResponse(&campaignv1.SendTestEmailResponse{}), nil
}
