package permit

import (
	"context"

	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	"connectrpc.com/connect"
	"github.com/google/uuid"
)

func (server *PermitService) DeletePermit(ctx context.Context, req *connect.Request[permitv1.DeletePermitRequest]) (*connect.Response[permitv1.DeletePermitResponse], error) {
	err := server.queries.DeletePermit(ctx, uuid.MustParse(req.Msg.Id))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&permitv1.DeletePermitResponse{
		Id: req.Msg.Id,
	}), nil
}
