package esclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/rs/zerolog/log"
)

// EnsureCustomerIndex creates the `phox_customers` index with the documented
// mapping if it doesn't already exist. Idempotent: returns nil if the index
// already exists. Errors are wrapped and returned; the caller decides whether
// to treat failures as fatal or enter degraded mode.
func EnsureCustomerIndex(ctx context.Context, client *elasticsearch.Client) error {
	existsRes, err := client.Indices.Exists(
		[]string{CustomerIndexName},
		client.Indices.Exists.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("check index exists: %w", err)
	}
	defer existsRes.Body.Close()

	if existsRes.StatusCode == 200 {
		log.Info().Str("index", CustomerIndexName).Msg("Elasticsearch index already exists")
		return nil
	}
	if existsRes.StatusCode != 404 {
		return fmt.Errorf("unexpected status from indices.exists: %d", existsRes.StatusCode)
	}

	createRes, err := client.Indices.Create(
		CustomerIndexName,
		client.Indices.Create.WithContext(ctx),
		client.Indices.Create.WithBody(strings.NewReader(CustomerIndexMapping)),
	)
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	defer createRes.Body.Close()

	if createRes.IsError() {
		return fmt.Errorf("create index returned error: %s", createRes.String())
	}

	log.Info().Str("index", CustomerIndexName).Msg("Elasticsearch index created")
	return nil
}

// RecreateCustomerIndex drops and re-creates the index. Used by the
// `reindex` CLI entrypoint to start from a clean slate.
func RecreateCustomerIndex(ctx context.Context, client *elasticsearch.Client) error {
	delRes, err := client.Indices.Delete(
		[]string{CustomerIndexName},
		client.Indices.Delete.WithContext(ctx),
		client.Indices.Delete.WithIgnoreUnavailable(true),
	)
	if err != nil {
		return fmt.Errorf("delete index: %w", err)
	}
	delRes.Body.Close()

	return EnsureCustomerIndex(ctx, client)
}
