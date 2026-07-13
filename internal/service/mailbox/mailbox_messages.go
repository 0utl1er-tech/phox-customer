package mailbox

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ListMailboxMessages returns ingested messages of a mailbox (metadata only —
// body is fetched via GetMailboxMessage). Requires viewer+ MailboxPermit.
func (s *MailboxService) ListMailboxMessages(
	ctx context.Context,
	req *connect.Request[mailboxv1.ListMailboxMessagesRequest],
) (*connect.Response[mailboxv1.ListMailboxMessagesResponse], error) {
	mailboxID, err := util.ParseUUID("mailbox_id", req.Msg.MailboxId)
	if err != nil {
		return nil, err
	}
	if err := s.authorizer.CheckMailboxPermission(ctx, mailboxID, db.RoleViewer); err != nil {
		return nil, err
	}

	limit := int32(50)
	if req.Msg.Limit != nil {
		limit = *req.Msg.Limit
	}
	var offset int32
	if req.Msg.Offset != nil {
		offset = *req.Msg.Offset
	}
	folder := pgtype.Text{}
	if req.Msg.Folder != nil && *req.Msg.Folder != "" {
		folder = pgtype.Text{String: *req.Msg.Folder, Valid: true}
	}

	rows, err := s.queries.ListMailboxMessages(ctx, db.ListMailboxMessagesParams{
		MailboxID: mailboxID,
		Folder:    folder,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list mailbox messages: %w", err))
	}
	total, err := s.queries.CountMailboxMessages(ctx, db.CountMailboxMessagesParams{
		MailboxID: mailboxID,
		Folder:    folder,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to count mailbox messages: %w", err))
	}

	out := make([]*mailboxv1.MailboxMessage, 0, len(rows))
	for _, r := range rows {
		m := &mailboxv1.MailboxMessage{
			Id:              r.ID.String(),
			MailboxId:       r.MailboxID.String(),
			Folder:          r.Folder,
			MessageId:       r.MessageID,
			FromAddr:        r.FromAddr,
			ToAddrs:         r.ToAddrs,
			CcAddrs:         r.CcAddrs,
			Subject:         r.Subject,
			AttachmentNames: r.AttachmentNames,
			OccurredAt:      timestamppb.New(r.OccurredAt),
		}
		if r.CustomerID.Valid {
			m.CustomerId = proto.String(uuid.UUID(r.CustomerID.Bytes).String())
		}
		out = append(out, m)
	}
	return connect.NewResponse(&mailboxv1.ListMailboxMessagesResponse{
		Messages: out,
		Total:    total,
	}), nil
}

// GetMailboxMessage returns one message including its body. Requires viewer+
// MailboxPermit on the mailbox the message belongs to.
func (s *MailboxService) GetMailboxMessage(
	ctx context.Context,
	req *connect.Request[mailboxv1.GetMailboxMessageRequest],
) (*connect.Response[mailboxv1.GetMailboxMessageResponse], error) {
	id, err := util.ParseUUID("id", req.Msg.Id)
	if err != nil {
		return nil, err
	}
	row, err := s.queries.GetMailboxMessage(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("mailbox message not found: %w", err))
	}
	// 認可はメッセージの属するメールボックスに対して行う。
	if err := s.authorizer.CheckMailboxPermission(ctx, row.MailboxID, db.RoleViewer); err != nil {
		return nil, err
	}

	m := &mailboxv1.MailboxMessage{
		Id:              row.ID.String(),
		MailboxId:       row.MailboxID.String(),
		Folder:          row.Folder,
		MessageId:       row.MessageID,
		FromAddr:        row.FromAddr,
		ToAddrs:         row.ToAddrs,
		CcAddrs:         row.CcAddrs,
		Subject:         row.Subject,
		BodyText:        row.BodyText,
		AttachmentNames: row.AttachmentNames,
		OccurredAt:      timestamppb.New(row.OccurredAt),
	}
	if row.CustomerID.Valid {
		m.CustomerId = proto.String(uuid.UUID(row.CustomerID.Bytes).String())
	}
	return connect.NewResponse(&mailboxv1.GetMailboxMessageResponse{Message: m}), nil
}
