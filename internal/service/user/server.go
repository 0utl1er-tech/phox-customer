package user

import db "github.com/0utl1er-tech/phox-customer/gen/sqlc"

type UserService struct {
	queries *db.Queries
}

func NewUserService(queries *db.Queries) *UserService {
	return &UserService{
		queries: queries,
	}
}
