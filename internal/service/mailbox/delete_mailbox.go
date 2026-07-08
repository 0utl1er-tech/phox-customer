package mailbox

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
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
	if err := s.queries.DeleteMailbox(ctx, mailboxID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete mailbox: %w", err))
	}
	return connect.NewResponse(&mailboxv1.DeleteMailboxResponse{Id: req.Msg.Id}), nil
}
