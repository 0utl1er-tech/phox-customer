package contact

import (
	"context"

	"connectrpc.com/connect"
	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/google/uuid"
)

func (server *ContactService) CreateContact(ctx context.Context, req *connect.Request[contactv1.CreateContactRequest]) (*connect.Response[contactv1.CreateContactResponse], error) {
	customerID, err := util.ParseUUID("customer_id", req.Msg.CustomerId)
	if err != nil {
		return nil, err
	}

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
		CustomerID: customerID,
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
		CreatedContact: modelToProto(result),
	}), nil
}
