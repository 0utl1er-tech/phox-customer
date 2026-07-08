package mailbox

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/jackc/pgx/v5/pgtype"
)

// UpdateMailbox changes display name / password / active flag. Owner only.
// An empty/unset password leaves the stored credential unchanged.
func (s *MailboxService) UpdateMailbox(
	ctx context.Context,
	req *connect.Request[mailboxv1.UpdateMailboxRequest],
) (*connect.Response[mailboxv1.UpdateMailboxResponse], error) {
	mailboxID, err := util.ParseUUID("id", req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if err := s.authorizer.CheckMailboxPermission(ctx, mailboxID, db.RoleOwner); err != nil {
		return nil, err
	}

	params := db.UpdateMailboxParams{ID: mailboxID}
	if req.Msg.DisplayName != nil {
		params.DisplayName = pgtype.Text{String: *req.Msg.DisplayName, Valid: true}
	}
	if req.Msg.Active != nil {
		params.Active = pgtype.Bool{Bool: *req.Msg.Active, Valid: true}
	}
	rotating := req.Msg.Password != nil && *req.Msg.Password != ""
	if rotating {
		enc, err := s.cipher.EncryptString(*req.Msg.Password)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to encrypt password: %w", err))
		}
		params.PasswordEnc = enc
		// mailu 側のパスワードも先に更新 (失敗したら DB は変えない)。
		if s.provisioner != nil {
			cur, gerr := s.queries.GetMailbox(ctx, mailboxID)
			if gerr != nil {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("mailbox not found: %w", gerr))
			}
			if perr := s.provisioner.SetPassword(ctx, cur.Address, *req.Msg.Password); perr != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("mailu set password: %w", perr))
			}
		}
	}

	mb, err := s.queries.UpdateMailbox(ctx, params)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update mailbox: %w", err))
	}

	// 呼び出し元は owner (上でチェック済み)。
	return connect.NewResponse(&mailboxv1.UpdateMailboxResponse{
		Mailbox: mailboxToProto(mb, db.RoleOwner),
	}), nil
}
