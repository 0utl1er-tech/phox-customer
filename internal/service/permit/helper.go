package permit

import (
	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
)

// modelToProto converts a db.Permit row to its proto representation.
func modelToProto(p db.Permit) *permitv1.Permit {
	return &permitv1.Permit{
		Id:     p.ID.String(),
		BookId: p.BookID.String(),
		UserId: p.UserID,
		Role:   util.ConvertDBRoleToProtoRole(p.Role),
	}
}
