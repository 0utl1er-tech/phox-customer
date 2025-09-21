package permit

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

type PermitService struct {
	queries *db.Queries
}

func NewPermitService(queries *db.Queries) *PermitService {
	return &PermitService{
		queries: queries,
	}
}
