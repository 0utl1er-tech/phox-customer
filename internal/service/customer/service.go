package customer

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/gemini"
	"github.com/0utl1er-tech/phox-customer/internal/search"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

// CustomerService customerサービスの実装
type CustomerService struct {
	queries    *db.Queries
	authorizer *auth.Authorizer
	indexer    *search.Indexer
	// Phase 27j: CSV 取り込みデータの AI 補完 (nil なら機能 disabled)。
	gemini *gemini.Client
}

// NewCustomerService creates a new customer service. `indexer` may have a
// nil ES client; in that case indexing is silently skipped (degraded mode).
// `gem` may be nil (GEMINI_API_KEY 未設定); EnrichCustomerRows は
// CodeFailedPrecondition を返し、GetEnrichmentStatus は enabled=false を返す。
func NewCustomerService(queries *db.Queries, indexer *search.Indexer, gem *gemini.Client) *CustomerService {
	return &CustomerService{
		queries:    queries,
		authorizer: auth.NewAuthorizer(queries),
		indexer:    indexer,
		gemini:     gem,
	}
}
