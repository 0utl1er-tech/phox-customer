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
// 認可: 対応する Book に editor 以上の権限 + Keycloak トークンに email claim
// (`email_verified=true`) が必要。
func (s *ActivityService) CreateActivityEmailSent(
	ctx context.Context,
	req *connect.Request[activityv1.CreateActivityEmailSentRequest],
) (*connect.Response[activityv1.CreateActivityEmailSentResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	// Keycloak トークンから email claim を取得 (Phase 11 で有効化)。
	// 現時点では private claim を読む最小実装。verify フラグは Phase 11 で追加。
	var fromEmail string
	if email, ok := token.PrivateClaims()["email"].(string); ok {
		fromEmail = email
	}
	if fromEmail == "" {
		return nil, connect.NewError(
			connect.CodeFailedPrecondition,
			fmt.Errorf("送信元のメールアドレス (Keycloak profile の email) が設定されていません"),
		)
	}

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

	// SMTP client が未設定なら 503 (Phase 10 では未設定が普通)
	if s.mailClient == nil {
		return nil, connect.NewError(
			connect.CodeUnavailable,
			fmt.Errorf("mail client not configured"),
		)
	}

	// Activity UUID を先に採番し Message-ID のベースに使う (dedup 安定化)
	activityID := uuid.New()
	messageID := fmt.Sprintf("phox-%s@phox.local", activityID.String())

	// 送信者名を User テーブルから取得 (From ヘッダの表示名に使う)
	fromName := ""
	if u, uerr := s.queries.GetUser(ctx, userID); uerr == nil {
		fromName = u.Name // 例: "黒羽晟"
	}

	// SMTP 送信 (先に副作用を起こし、成功したら DB insert)
	ccs := []string{}
	if req.Msg.MailCc != nil && *req.Msg.MailCc != "" {
		ccs = append(ccs, *req.Msg.MailCc)
	}
	if err := s.mailClient.Send(
		ctx,
		fromEmail,
		fromName,
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
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create activity: %w", err))
	}

	return connect.NewResponse(&activityv1.CreateActivityEmailSentResponse{
		Activity: modelToProto(act),
	}), nil
}
