package contact

import (
	"context"

	"connectrpc.com/connect"
	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (server *ContactService) UpdateContact(ctx context.Context, req *connect.Request[contactv1.UpdateContactRequest]) (*connect.Response[contactv1.UpdateContactResponse], error) {
	params := db.UpdateContactParams{
		ID: uuid.MustParse(req.Msg.Id),
	}

	if req.Msg.Name != nil {
		params.Name = pgtype.Text{String: *req.Msg.Name, Valid: true}
	}
	if req.Msg.Sex != nil {
		params.Sex = pgtype.Text{String: *req.Msg.Sex, Valid: true}
	}
	if req.Msg.Phone != nil {
		params.Phone = pgtype.Text{String: *req.Msg.Phone, Valid: true}
	}
	if req.Msg.Mail != nil {
		params.Mail = pgtype.Text{String: *req.Msg.Mail, Valid: true}
	}
	if req.Msg.Fax != nil {
		params.Fax = pgtype.Text{String: *req.Msg.Fax, Valid: true}
	}

	result, err := server.queries.UpdateContact(ctx, params)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&contactv1.UpdateContactResponse{
		UpdatedContact: &contactv1.Contact{
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
