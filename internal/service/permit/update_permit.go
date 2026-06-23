package permit

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	util "github.com/0utl1er-tech/phox-customer/internal/util"
)

func (server *PermitService) UpdatePermit(ctx context.Context, req *connect.Request[permitv1.UpdatePermitRequest]) (*connect.Response[permitv1.UpdatePermitResponse], error) {
	if req.Msg.Role == permitv1.Role_ROLE_UNSPECIFIED {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("role is required"))
	}
	id, err := util.ParseUUID("id", req.Msg.Id)
	if err != nil {
		return nil, err
	}

	result, err := server.queries.UpdatePermit(ctx, db.UpdatePermitParams{
		ID: id,
		Role: db.NullRole{
			Role:  util.ConvertProtoRoleToDBRole(req.Msg.Role),
			Valid: true,
		},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&permitv1.UpdatePermitResponse{
		UpdatedPermit: modelToProto(result),
	}), nil
}
