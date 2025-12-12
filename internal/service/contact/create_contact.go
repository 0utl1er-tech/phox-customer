package contact

import (
	"context"
	"fmt"

	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	staffv1 "github.com/0utl1er-tech/phox-customer/gen/pb/staff/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"connectrpc.com/connect"
	"github.com/google/uuid"
)

func (server *ContactService) CreateContact(ctx context.Context, req *connect.Request[contactv1.CreateContactRequest]) (*connect.Response[contactv1.CreateContactResponse], error) {
	staff, err := server.queries.GetStaff(ctx, uuid.MustParse(req.Msg.StaffId))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("staffが存在しません: %w", err))
	}
	result, err := server.queries.CreateContact(ctx, db.CreateContactParams{
		ID:         uuid.New(),
		CustomerID: uuid.MustParse(req.Msg.CustomerId),
		StaffID:    uuid.MustParse(req.Msg.StaffId),
		Phone:      *req.Msg.Phone,
		Mail:       *req.Msg.Mail,
		Fax:        *req.Msg.Fax,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&contactv1.CreateContactResponse{
		CreatedContact: &contactv1.Contact{
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
