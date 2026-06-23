package permit

import (
	"context"

	"connectrpc.com/connect"
	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	"github.com/0utl1er-tech/phox-customer/internal/util"
)

func (server *PermitService) DeletePermit(ctx context.Context, req *connect.Request[permitv1.DeletePermitRequest]) (*connect.Response[permitv1.DeletePermitResponse], error) {
	id, err := util.ParseUUID("id", req.Msg.Id)
	if err != nil {
		return nil, err
	}

	if err := server.queries.DeletePermit(ctx, id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&permitv1.DeletePermitResponse{
		Id: req.Msg.Id,
	}), nil
}
