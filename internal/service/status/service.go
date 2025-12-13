package status

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

type StatusService struct {
	queries    *db.Queries
	authorizer *auth.Authorizer
}

func NewStatusService(queries *db.Queries) *StatusService {
	return &StatusService{
		queries:    queries,
		authorizer: auth.NewAuthorizer(queries),
	}
}
