package book

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

type BookService struct {
	queries *db.Queries
}

func NewBookService(queries *db.Queries) *BookService {
	return &BookService{
		queries: queries,
	}
}
