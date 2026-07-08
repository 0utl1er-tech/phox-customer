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
	if req.Msg.Password != nil && *req.Msg.Password != "" {
		enc, err := s.cipher.EncryptString(*req.Msg.Password)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to encrypt password: %w", err))
		}
		params.PasswordEnc = enc
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
