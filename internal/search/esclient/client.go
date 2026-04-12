// Package esclient wraps the official go-elasticsearch/v8 client with phox-
// customer-specific conveniences: single factory, fail-soft behavior for
// bootstrapping, and graceful nil-handling so the rest of the backend can run
// in "degraded mode" when Elasticsearch is unreachable.
package esclient

import (
	"errors"

	"github.com/elastic/go-elasticsearch/v8"
)

// CustomerIndexName is the single-index name used across the app for now.
// Future multi-env deploys should prefix this via environment variable.
const CustomerIndexName = "phox_customers"

// NewClient builds an elasticsearch.Client for the given URL. Returns an
// error if the URL is empty; otherwise constructs a client (may not actually
// be reachable — callers must ping if they need a connectivity guarantee).
func NewClient(url string) (*elasticsearch.Client, error) {
	if url == "" {
		return nil, errors.New("esclient: ELASTICSEARCH_URL is empty")
	}
	cfg := elasticsearch.Config{Addresses: []string{url}}
	return elasticsearch.NewClient(cfg)
}
