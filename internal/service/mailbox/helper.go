package mailbox

import (
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// listRowToProto converts a ListMailboxesByUserID row (mailbox + caller role)
// to proto. Never includes the password.
func listRowToProto(r db.ListMailboxesByUserIDRow) *mailboxv1.Mailbox {
	return &mailboxv1.Mailbox{
		Id:           r.ID.String(),
		Address:      r.Address,
		DisplayName:  r.DisplayName,
		SmtpUsername: r.SmtpUsername,
		Active:       r.Active,
		Role:         util.ConvertDBRoleToProtoRole(r.Role),
		CreatedAt:    timestamppb.New(r.CreatedAt),
		UpdatedAt:    timestamppb.New(r.UpdatedAt),
	}
}

// mailboxToProto converts a raw Mailbox row plus the caller's role. Password
// is intentionally dropped.
func mailboxToProto(m db.Mailbox, role db.Role) *mailboxv1.Mailbox {
	return &mailboxv1.Mailbox{
		Id:           m.ID.String(),
		Address:      m.Address,
		DisplayName:  m.DisplayName,
		SmtpUsername: m.SmtpUsername,
		Active:       m.Active,
		Role:         util.ConvertDBRoleToProtoRole(role),
		CreatedAt:    timestamppb.New(m.CreatedAt),
		UpdatedAt:    timestamppb.New(m.UpdatedAt),
	}
}

func permitRowToMailboxUser(r db.ListMailboxPermitsWithUserInfoRow) *mailboxv1.MailboxUser {
	return &mailboxv1.MailboxUser{
		PermitId: r.ID.String(),
		UserId:   r.UserID,
		UserName: r.UserName,
		Role:     util.ConvertDBRoleToProtoRole(r.Role),
	}
}
