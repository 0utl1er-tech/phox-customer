package mailtemplate

import (
	mailtemplatev1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailtemplate/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func modelToProto(m db.MailTemplate) *mailtemplatev1.MailTemplate {
	return &mailtemplatev1.MailTemplate{
		Id:        m.ID.String(),
		BookId:    m.BookID.String(),
		Name:      m.Name,
		Subject:   m.Subject,
		Body:      m.Body,
		CreatedAt: timestamppb.New(m.CreatedAt),
		UpdatedAt: timestamppb.New(m.UpdatedAt),
	}
}
