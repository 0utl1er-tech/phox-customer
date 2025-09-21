package contact

import db "github.com/0utl1er-tech/phox-customer/gen/sqlc"

type ContactService struct {
	queries *db.Queries
}

func NewContactService(queries *db.Queries) *ContactService {
	return &ContactService{queries: queries}
}
