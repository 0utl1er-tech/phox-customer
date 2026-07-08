package contact

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
)

func (s *ContactService) ListContact(
	ctx context.Context,
	req *connect.Request[contactv1.ListContactRequest],
) (*connect.Response[contactv1.ListContactResponse], error) {
	_, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	customerID, err := uuid.Parse(req.Msg.CustomerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("無効なcustomer_idです: %w", err))
	}

	contacts, err := s.queries.ListContacts(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("連絡先の取得に失敗しました: %w", err))
	}

	contactList := make([]*contactv1.Contact, 0, len(contacts))
	for _, contact := range contacts {
		contactList = append(contactList, modelToProto(contact))
	}

	return connect.NewResponse(&contactv1.ListContactResponse{
		Contacts: contactList,
	}), nil
}
