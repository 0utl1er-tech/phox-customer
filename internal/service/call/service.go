package call

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

type CallService struct {
	queries    *db.Queries
	authorizer *auth.Authorizer
}

func NewCallService(queries *db.Queries) *CallService {
	return &CallService{
		queries:    queries,
		authorizer: auth.NewAuthorizer(queries),
	}
}
