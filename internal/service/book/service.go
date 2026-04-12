package book

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/search"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

type BookService struct {
	queries    *db.Queries
	authorizer *auth.Authorizer
	indexer    *search.Indexer
}

// NewBookService creates a new book service. `indexer` may have a nil ES
// client; in that case ImportBook's bulk indexing is silently skipped.
func NewBookService(queries *db.Queries, indexer *search.Indexer) *BookService {
	return &BookService{
		queries:    queries,
		authorizer: auth.NewAuthorizer(queries),
		indexer:    indexer,
	}
}
