package contact

import (
	"context"

	"connectrpc.com/connect"
	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
)

func (server *ContactService) CreateContact(ctx context.Context, req *connect.Request[contactv1.CreateContactRequest]) (*connect.Response[contactv1.CreateContactResponse], error) {
	var phone, mail, fax string
	if req.Msg.Phone != nil {
		phone = *req.Msg.Phone
	}
	if req.Msg.Mail != nil {
		mail = *req.Msg.Mail
	}
	if req.Msg.Fax != nil {
		fax = *req.Msg.Fax
	}

	result, err := server.queries.CreateContact(ctx, db.CreateContactParams{
		ID:         uuid.New(),
		CustomerID: uuid.MustParse(req.Msg.CustomerId),
		Name:       req.Msg.Name,
		Sex:        req.Msg.Sex,
		Phone:      phone,
		Mail:       mail,
		Fax:        fax,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&contactv1.CreateContactResponse{
		CreatedContact: &contactv1.Contact{
			Id:         result.ID.String(),
			CustomerId: result.CustomerID.String(),
			Name:       result.Name,
			Sex:        result.Sex,
			Phone:      result.Phone,
			Mail:       result.Mail,
			Fax:        result.Fax,
		},
	}), nil
}
