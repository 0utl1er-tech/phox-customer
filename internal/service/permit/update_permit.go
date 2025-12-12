package permit

import (
	"context"
	"fmt"

	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	util "github.com/0utl1er-tech/phox-customer/internal/util"
	"connectrpc.com/connect"
	"github.com/google/uuid"
)

func (server *PermitService) UpdatePermit(ctx context.Context, req *connect.Request[permitv1.UpdatePermitRequest]) (*connect.Response[permitv1.UpdatePermitResponse], error) {
	if req.Msg.Role == permitv1.Role_ROLE_UNSPECIFIED {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("role is required"))
	}
	result, err := server.queries.UpdatePermit(ctx, db.UpdatePermitParams{
		ID: uuid.MustParse(req.Msg.Id),
		Role: db.NullRole{
			Role:  util.ConvertProtoRoleToDBRole(req.Msg.Role),
			Valid: req.Msg.Role != permitv1.Role_ROLE_UNSPECIFIED,
		},
	})

	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&permitv1.UpdatePermitResponse{
		UpdatedPermit: &permitv1.Permit{
			Id:     result.ID.String(),
			BookId: result.BookID.String(),
			UserId: result.UserID,
			Role:   util.ConvertDBRoleToProtoRole(result.Role),
		},
	}), nil
}
