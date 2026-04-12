package search

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/elastic/go-elasticsearch/v8"
)

// SearchService implements searchv1connect.SearchServiceHandler.
//
// It resolves the caller's accessible books from the application DB (Permit
// table) and then issues the Elasticsearch query scoped to those books, so
// users cannot see other tenants' customers even if they pass arbitrary
// book_ids in the request.
type SearchService struct {
	queries *db.Queries
	es      *elasticsearch.Client
}

func NewSearchService(queries *db.Queries, es *elasticsearch.Client) *SearchService {
	return &SearchService{queries: queries, es: es}
}
