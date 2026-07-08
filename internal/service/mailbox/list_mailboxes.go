package mailbox

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

// ListMailboxes returns the mailboxes the caller has any MailboxPermit on,
// each annotated with the caller's role. No passwords are returned.
func (s *MailboxService) ListMailboxes(
	ctx context.Context,
	req *connect.Request[mailboxv1.ListMailboxesRequest],
) (*connect.Response[mailboxv1.ListMailboxesResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := s.queries.ListMailboxesByUserID(ctx, token.Subject())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list mailboxes: %w", err))
	}

	out := make([]*mailboxv1.Mailbox, 0, len(rows))
	for _, r := range rows {
		out = append(out, listRowToProto(r))
	}
	return connect.NewResponse(&mailboxv1.ListMailboxesResponse{Mailboxes: out}), nil
}
