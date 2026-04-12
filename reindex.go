package main

import (
	"context"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/search"
	"github.com/0utl1er-tech/phox-customer/internal/search/esclient"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// runReindex drops and recreates the `phox_customers` index and bulk-indexes
// every customer row in the application DB. Called via `go run . reindex`.
//
// This is a maintenance CLI — not exposed as an RPC — so it skips permit
// checks and indexes all customers globally. Operators run it after schema
// changes, ES upgrades, or disaster recovery.
func runReindex(cfg util.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if cfg.ElasticsearchURL == "" {
		log.Fatal().Msg("reindex: ELASTICSEARCH_URL is not set")
	}

	// DB
	pool, err := pgxpool.New(ctx, cfg.DBSource)
	if err != nil {
		log.Fatal().Err(err).Msg("reindex: failed to connect to db")
	}
	defer pool.Close()
	queries := db.New(pool)

	// ES
	esClient, err := esclient.NewClient(cfg.ElasticsearchURL)
	if err != nil {
		log.Fatal().Err(err).Msg("reindex: failed to create es client")
	}

	if err := esclient.RecreateCustomerIndex(ctx, esClient); err != nil {
		log.Fatal().Err(err).Msg("reindex: failed to recreate index")
	}

	customers, err := queries.ListAllCustomers(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("reindex: failed to list all customers")
	}

	log.Info().Int("total", len(customers)).Msg("reindex: bulk indexing customers")

	indexer := search.NewIndexer(esClient)
	const batch = 500
	for i := 0; i < len(customers); i += batch {
		end := i + batch
		if end > len(customers) {
			end = len(customers)
		}
		docs := make([]search.CustomerDoc, 0, end-i)
		for _, c := range customers[i:end] {
			docs = append(docs, search.NewCustomerDoc(
				c.ID, c.BookID, c.Name, c.Corporation, c.Address, c.Memo, c.Phone, c.Category,
				c.UpdatedAt,
			))
		}
		if err := indexer.BulkIndex(ctx, docs); err != nil {
			log.Error().Err(err).Int("batch_start", i).Msg("reindex: batch failed")
			continue
		}
		log.Info().Int("indexed", end).Int("total", len(customers)).Msg("reindex: progress")
	}

	log.Info().Msg("reindex: done")
}

// newESClientOrWarn creates an ES client from config. If URL is empty or the
// client init fails, returns nil and logs a warning. Used by main() so that a
// broken ES doesn't prevent the rest of the backend from starting.
func newESClientOrWarn(cfg util.Config) *elasticsearch.Client {
	if cfg.ElasticsearchURL == "" {
		log.Warn().Msg("ELASTICSEARCH_URL not set — search features disabled (degraded mode)")
		return nil
	}
	client, err := esclient.NewClient(cfg.ElasticsearchURL)
	if err != nil {
		log.Warn().Err(err).Msg("failed to create es client — search features disabled")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := esclient.EnsureCustomerIndex(ctx, client); err != nil {
		log.Warn().Err(err).Msg("failed to ensure es index — search features may be degraded")
		// Return the client anyway — per-call operations will still work once ES recovers.
	}
	return client
}
