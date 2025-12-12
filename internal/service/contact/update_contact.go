package contact

import (
	"context"
	"fmt"

	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	staffv1 "github.com/0utl1er-tech/phox-customer/gen/pb/staff/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (server *ContactService) UpdateContact(ctx context.Context, req *connect.Request[contactv1.UpdateContactRequest]) (*connect.Response[contactv1.UpdateContactResponse], error) {
	var staff db.Staff
	var err error
	if req.Msg.StaffId != nil {
		staff, err = server.queries.GetStaff(ctx, uuid.MustParse(*req.Msg.StaffId))
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("staffが存在しません: %w", err))
		}
	}
	result, err := server.queries.UpdateContact(ctx, db.UpdateContactParams{
		ID:      uuid.MustParse(req.Msg.Id),
		StaffID: pgtype.UUID{Bytes: uuid.MustParse(*req.Msg.StaffId), Valid: req.Msg.StaffId != nil},
		Phone:   pgtype.Text{String: *req.Msg.Phone, Valid: req.Msg.Phone != nil},
		Mail:    pgtype.Text{String: *req.Msg.Mail, Valid: req.Msg.Mail != nil},
		Fax:     pgtype.Text{String: *req.Msg.Fax, Valid: req.Msg.Fax != nil},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&contactv1.UpdateContactResponse{
		UpdatedContact: &contactv1.Contact{
			Id:         result.ID.String(),
			CustomerId: result.CustomerID.String(),
			Staff: &staffv1.Staff{
				Id:   result.StaffID.String(),
				Name: staff.Name,
				Sex:  staff.Sex,
			},
			Phone: result.Phone,
			Mail:  result.Mail,
			Fax:   result.Fax,
		},
	}), nil
}
