package mailbox

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
)

// CreateMailbox registers a real mailbox (mailu account) the company owns.
// Any authenticated company user may register one; the creator automatically
// receives an owner MailboxPermit (mirrors CreateBook). The password is
// encrypted at rest and never returned.
func (s *MailboxService) CreateMailbox(
	ctx context.Context,
	req *connect.Request[mailboxv1.CreateMailboxRequest],
) (*connect.Response[mailboxv1.CreateMailboxResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	callerID := token.Subject()

	caller, err := s.queries.GetUser(ctx, callerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get caller user: %w", err))
	}

	smtpUsername := strings.TrimSpace(req.Msg.SmtpUsername)
	if smtpUsername == "" {
		smtpUsername = req.Msg.Address
	}

	passwordEnc, err := s.cipher.EncryptString(req.Msg.Password)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to encrypt password: %w", err))
	}

	mailboxID := uuid.New()
	mb, err := s.queries.CreateMailbox(ctx, db.CreateMailboxParams{
		ID:           mailboxID,
		CompanyID:    caller.CompanyID,
		Address:      req.Msg.Address,
		DisplayName:  req.Msg.DisplayName,
		SmtpUsername: smtpUsername,
		PasswordEnc:  passwordEnc,
		Active:       true,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("このアドレスのメールボックスは既に登録されています"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create mailbox: %w", err))
	}

	// 作成者を owner として登録。
	if _, err := s.queries.CreateMailboxPermit(ctx, db.CreateMailboxPermitParams{
		ID:        uuid.New(),
		MailboxID: mailboxID,
		UserID:    callerID,
		Role:      db.RoleOwner,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create owner permit: %w", err))
	}

	return connect.NewResponse(&mailboxv1.CreateMailboxResponse{
		Mailbox: mailboxToProto(mb, db.RoleOwner),
	}), nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "23505") ||
		strings.Contains(s, "duplicate key") ||
		strings.Contains(s, "unique constraint")
}
