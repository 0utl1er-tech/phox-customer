package book

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

type BookService struct {
	queries    *db.Queries
	authorizer *auth.Authorizer
}

func NewBookService(queries *db.Queries) *BookService {
	return &BookService{
		queries:    queries,
		authorizer: auth.NewAuthorizer(queries),
	}
}
