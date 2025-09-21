package permit

import (
	"context"

	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
)

func (server *PermitService) DeletePermit(ctx context.Context, req *connect.Request[permitv1.DeletePermitRequest]) (*connect.Response[permitv1.DeletePermitResponse], error) {
	err := server.queries.DeletePermit(ctx, uuid.MustParse(req.Msg.Id))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return nil, nil
}
