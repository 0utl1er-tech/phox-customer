package contact

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	"github.com/0utl1er-tech/phox-customer/internal/util"
)

func (server *ContactService) DeleteContact(ctx context.Context, req *connect.Request[contactv1.DeleteContactRequest]) (*connect.Response[contactv1.DeleteContactResponse], error) {
	id, err := util.ParseUUID("id", req.Msg.Id)
	if err != nil {
		return nil, err
	}

	if err := server.queries.DeleteContact(ctx, id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("contactが存在しません: %w", err))
	}
	return connect.NewResponse(&contactv1.DeleteContactResponse{
		Id: req.Msg.Id,
	}), nil
}
