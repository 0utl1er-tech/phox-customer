package contact

import (
	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

// modelToProto converts a db.Contact row to its proto representation.
func modelToProto(c db.Contact) *contactv1.Contact {
	return &contactv1.Contact{
		Id:         c.ID.String(),
		CustomerId: c.CustomerID.String(),
		Name:       c.Name,
		Sex:        c.Sex,
		Phone:      c.Phone,
		Mail:       c.Mail,
		Fax:        c.Fax,
	}
}
