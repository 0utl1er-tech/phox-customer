package mailbox

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/mailu"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// generatePassword は mailu アカウント用の強いランダムパスワードを返す
// (32 byte → base64url、約 43 文字)。Phox だけが保持する。
func generatePassword() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

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

	// パスワードの決定:
	//  - provisioner あり: 指定が無ければ Phox がランダム生成 (人はパスワードを
	//    知らなくてよい = Phox が完全に支配)。mailu にアカウントを作成する。
	//  - provisioner なし: 既存アカウント登録モード。パスワードは必須。
	password := req.Msg.Password
	if password == "" {
		if s.provisioner == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("パスワードを指定してください (mailu 自動作成が無効なため)"))
		}
		gen, gerr := generatePassword()
		if gerr != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("generate password: %w", gerr))
		}
		password = gen
	}

	if s.provisioner != nil {
		// mailu にアカウントを作成 (enable_imap=true, allow_spoofing=false)。
		if perr := s.provisioner.CreateUser(ctx, req.Msg.Address, password, req.Msg.DisplayName); perr != nil {
			if errors.Is(perr, mailu.ErrConflict) {
				return nil, connect.NewError(connect.CodeAlreadyExists,
					fmt.Errorf("このアドレスは mailu に既に存在します: %s", req.Msg.Address))
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("mailu provision: %w", perr))
		}
	}

	passwordEnc, err := s.cipher.EncryptString(password)
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
		// DB 挿入に失敗したら、直前に作った mailu アカウントを消して孤児を防ぐ。
		if s.provisioner != nil {
			if derr := s.provisioner.DeleteUser(ctx, req.Msg.Address); derr != nil {
				log.Warn().Err(derr).Str("address", req.Msg.Address).
					Msg("mailbox: failed to roll back mailu account after DB error")
			}
		}
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
