package mailbox

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/mailu"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/rs/zerolog/log"
)

// DeleteMailbox removes the mailbox (and cascades its permits). Owner only.
// Activity rows keep their history (mailbox_id → NULL via ON DELETE SET NULL).
func (s *MailboxService) DeleteMailbox(
	ctx context.Context,
	req *connect.Request[mailboxv1.DeleteMailboxRequest],
) (*connect.Response[mailboxv1.DeleteMailboxResponse], error) {
	mailboxID, err := util.ParseUUID("id", req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if err := s.authorizer.CheckMailboxPermission(ctx, mailboxID, db.RoleOwner); err != nil {
		return nil, err
	}
	// mailu アカウントも削除するため、行を消す前にアドレスを控える。
	mb, gerr := s.queries.GetMailbox(ctx, mailboxID)
	if gerr != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("mailbox not found: %w", gerr))
	}
	if err := s.queries.DeleteMailbox(ctx, mailboxID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete mailbox: %w", err))
	}
	// mailu アカウント削除は best-effort (既に無ければ無視)。DB 側は既に消えて
	// いるので、ここで失敗しても Phox 上は削除済み。
	if s.provisioner != nil {
		if derr := s.provisioner.DeleteUser(ctx, mb.Address); derr != nil && !errors.Is(derr, mailu.ErrNotFound) {
			log.Warn().Err(derr).Str("address", mb.Address).Msg("mailbox: mailu account delete failed")
		}
	}
	return connect.NewResponse(&mailboxv1.DeleteMailboxResponse{Id: req.Msg.Id}), nil
}
