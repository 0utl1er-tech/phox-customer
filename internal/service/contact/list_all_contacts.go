package contact

import (
	"context"

	"connectrpc.com/connect"
	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

func (s *ContactService) ListAllContacts(
	ctx context.Context,
	req *connect.Request[contactv1.ListAllContactsRequest],
) (*connect.Response[contactv1.ListAllContactsResponse], error) {
	limit := req.Msg.Limit
	if limit == 0 || limit > 100 {
		limit = 100
	}
	offset := req.Msg.Offset

	rows, err := s.queries.ListAllContacts(ctx, db.ListAllContactsParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var contacts []*contactv1.ContactWithCustomer
	for _, row := range rows {
		contacts = append(contacts, &contactv1.ContactWithCustomer{
			Id:         row.ID.String(),
			CustomerId: row.CustomerID.String(),
			Name:       row.Name,
			Sex:        row.Sex,
			Phone:      row.Phone,
			Mail:       row.Mail,
			Fax:        row.Fax,
			Customer: &contactv1.CustomerSummary{
				Id:          row.CustomerID.String(),
				Name:        row.CustomerName,
				Corporation: row.CustomerCorporation,
			},
		})
	}

	return connect.NewResponse(&contactv1.ListAllContactsResponse{
		Contacts: contacts,
	}), nil
}
