package activity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/mail"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// CreateActivityEmailSent は Activity を insert する前に SMTP 送信を行い、
// 成功したら Activity を作成する。送信失敗時は DB を汚さない (副作用の先行)。
//
// 送信経路は 2 系統 (Phase 25):
//   - mailbox_id 指定: Phox が所有する実メールボックスとして送信。
//     From = メールボックスのアドレス、SMTP 認証もそのメールボックスの資格情報。
//     呼び出しユーザーに editor 以上の MailboxPermit が必要。Reply-To は付けない
//     (返信は同じメールボックスに届き、IMAP 取込みが拾う)。
//   - 省略 (レガシー): サービスアカウントで SMTP 認証し、From をトークンの
//     email claim になりすます。Reply-To はサービスアカウント固定。
//
// 認可: 両経路とも対象顧客の Book に editor 以上の権限が必要。
func (s *ActivityService) CreateActivityEmailSent(
	ctx context.Context,
	req *connect.Request[activityv1.CreateActivityEmailSentRequest],
) (*connect.Response[activityv1.CreateActivityEmailSentResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	customerID, err := uuid.Parse(req.Msg.CustomerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid customer_id: %w", err))
	}

	// Customer → Book で editor 権限チェック
	customer, err := s.queries.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("customer not found: %w", err))
	}
	if err := s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleEditor); err != nil {
		return nil, err
	}

	// optional contact_id
	contactID := pgtype.UUID{Valid: false}
	if req.Msg.ContactId != nil && *req.Msg.ContactId != "" {
		cid, err := uuid.Parse(*req.Msg.ContactId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid contact_id: %w", err))
		}
		contactID = pgtype.UUID{Bytes: cid, Valid: true}
	}

	// Activity UUID を先に採番し Message-ID のベースに使う (dedup 安定化)
	activityID := uuid.New()
	messageID := fmt.Sprintf("phox-%s@phox.local", activityID.String())

	// 操作ユーザーの表示名 (Activity/レスポンスの user_name)。
	senderUserName := ""
	if u, uerr := s.queries.GetUser(ctx, userID); uerr == nil {
		senderUserName = u.Name // 例: "黒羽晟"
	}

	ccs := []string{}
	if req.Msg.MailCc != nil && *req.Msg.MailCc != "" {
		ccs = append(ccs, *req.Msg.MailCc)
	}

	var fromEmail string
	mailboxID := pgtype.UUID{Valid: false}

	if req.Msg.MailboxId != nil && *req.Msg.MailboxId != "" {
		// ── 実メールボックス送信 (Phase 25) ──────────────────────
		if s.mailboxSender == nil || s.mailboxCipher == nil {
			return nil, connect.NewError(connect.CodeUnavailable,
				fmt.Errorf("mailbox sending is not configured (MAILU_SMTP_* / MAILBOX_SECRET_KEY)"))
		}
		mbID, perr := uuid.Parse(*req.Msg.MailboxId)
		if perr != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid mailbox_id: %w", perr))
		}
		// 送信にはそのメールボックスの editor 以上が必要。
		if err := s.authorizer.CheckMailboxPermission(ctx, mbID, db.RoleEditor); err != nil {
			return nil, err
		}
		mb, gerr := s.queries.GetMailbox(ctx, mbID)
		if gerr != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("mailbox not found: %w", gerr))
		}
		if !mb.Active {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				fmt.Errorf("このメールボックスは無効化されています"))
		}
		password, derr := s.mailboxCipher.DecryptString(mb.PasswordEnc)
		if derr != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("decrypt mailbox password: %w", derr))
		}

		fromEmail = mb.Address
		fromName := mb.DisplayName
		if fromName == "" {
			fromName = senderUserName
		}
		if err := s.mailboxSender.SendAs(
			ctx,
			mb.SmtpUsername, password,
			mb.Address, fromName,
			req.Msg.MailTo, ccs,
			req.Msg.Subject, req.Msg.Body, messageID,
		); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("smtp send (mailbox): %w", err))
		}
		mailboxID = pgtype.UUID{Bytes: mbID, Valid: true}
	} else {
		// ── レガシー: なりすまし送信 ─────────────────────────────
		// Keycloak トークンから email claim を取得し From ヘッダに使う。
		if email, ok := token.PrivateClaims()["email"].(string); ok {
			fromEmail = email
		}
		if fromEmail == "" {
			return nil, connect.NewError(
				connect.CodeFailedPrecondition,
				fmt.Errorf("送信元のメールアドレス (Keycloak profile の email) が設定されていません"),
			)
		}
		if s.mailClient == nil {
			return nil, connect.NewError(
				connect.CodeUnavailable,
				fmt.Errorf("mail client not configured"),
			)
		}
		if err := s.mailClient.Send(
			ctx,
			fromEmail,
			senderUserName,
			req.Msg.MailTo,
			ccs,
			req.Msg.Subject,
			req.Msg.Body,
			messageID,
		); err != nil {
			if errors.Is(err, mail.ErrNotConfigured) {
				return nil, connect.NewError(connect.CodeUnavailable, err)
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("smtp send: %w", err))
		}
	}

	// DB 挿入
	ccPtr := pgtype.Text{Valid: false}
	if req.Msg.MailCc != nil && *req.Msg.MailCc != "" {
		ccPtr = pgtype.Text{String: *req.Msg.MailCc, Valid: true}
	}
	act, err := s.queries.CreateActivity(ctx, db.CreateActivityParams{
		ID:         activityID,
		CustomerID: customerID,
		ContactID:  contactID,
		Type:       "email_sent",
		UserID:     userID,
		StatusID:   pgtype.UUID{Valid: false},
		MailFrom:   pgtype.Text{String: fromEmail, Valid: true},
		MailTo:     pgtype.Text{String: req.Msg.MailTo, Valid: true},
		MailCc:     ccPtr,
		Subject:    pgtype.Text{String: req.Msg.Subject, Valid: true},
		Body:       pgtype.Text{String: req.Msg.Body, Valid: true},
		MessageID:  pgtype.Text{String: messageID, Valid: true},
		OccurredAt: time.Now(),
		MailboxID:  mailboxID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create activity: %w", err))
	}

	// modelToProto は INSERT...RETURNING の生 Activity 行 (User JOIN 無し) を
	// 変換するので user_name が空になる。操作ユーザー名は上で GetUser 済みなので
	// レスポンスにも載せる (UI の楽観的表示 / MCP send_customer_email が使う)。
	actProto := modelToProto(act)
	actProto.UserName = senderUserName
	return connect.NewResponse(&activityv1.CreateActivityEmailSentResponse{
		Activity: actProto,
	}), nil
}
