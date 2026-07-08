package contact

import (
	"context"

	"connectrpc.com/connect"
	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
)

func (server *ContactService) UpdateContact(ctx context.Context, req *connect.Request[contactv1.UpdateContactRequest]) (*connect.Response[contactv1.UpdateContactResponse], error) {
	id, err := util.ParseUUID("id", req.Msg.Id)
	if err != nil {
		return nil, err
	}

	// 空文字列は「未指定」として既存値を保持する (util.OptionalText の doc 参照)
	result, err := server.queries.UpdateContact(ctx, db.UpdateContactParams{
		ID:    id,
		Name:  util.OptionalText(req.Msg.Name),
		Sex:   util.OptionalText(req.Msg.Sex),
		Phone: util.OptionalText(req.Msg.Phone),
		Mail:  util.OptionalText(req.Msg.Mail),
		Fax:   util.OptionalText(req.Msg.Fax),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&contactv1.UpdateContactResponse{
		UpdatedContact: modelToProto(result),
	}), nil
}
