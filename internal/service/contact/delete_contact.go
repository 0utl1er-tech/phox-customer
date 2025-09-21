package contact

import (
	"context"

	"fmt"

	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
)

func (server *ContactService) DeleteContact(ctx context.Context, req *connect.Request[contactv1.DeleteContactRequest]) (*connect.Response[contactv1.DeleteContactResponse], error) {
	err := server.queries.DeleteContact(ctx, uuid.MustParse(req.Msg.Id))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("contactが存在しません: %w", err))
	}
	return connect.NewResponse(&contactv1.DeleteContactResponse{
		Id: req.Msg.Id,
	}), nil
}
