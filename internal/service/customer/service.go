package customer

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/search"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

// CustomerService customerサービスの実装
type CustomerService struct {
	queries    *db.Queries
	authorizer *auth.Authorizer
	indexer    *search.Indexer
}

// NewCustomerService creates a new customer service. `indexer` may have a
// nil ES client; in that case indexing is silently skipped (degraded mode).
func NewCustomerService(queries *db.Queries, indexer *search.Indexer) *CustomerService {
	return &CustomerService{
		queries:    queries,
		authorizer: auth.NewAuthorizer(queries),
		indexer:    indexer,
	}
}
