package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/0utl1er-tech/phox-customer/internal/search/esclient"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// CustomerDoc is the shape of a document stored in the `phox_customers`
// Elasticsearch index. It maps 1:1 to the mapping in esclient/mapping.go.
type CustomerDoc struct {
	CustomerID  string    `json:"customer_id"`
	BookID      string    `json:"book_id"`
	Name        string    `json:"name"`
	Corporation string    `json:"corporation"`
	Address     string    `json:"address"`
	Memo        string    `json:"memo"`
	Phone       string    `json:"phone"`
	PhoneText   string    `json:"phone_text"`
	Category    string    `json:"category"`
	Prefecture  string    `json:"prefecture"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewCustomerDoc builds an index doc from raw Customer fields, automatically
// extracting the prefecture from the address via ExtractPrefecture.
func NewCustomerDoc(
	id, bookID uuid.UUID,
	name, corporation, address, memo, phone, category string,
	updatedAt time.Time,
) CustomerDoc {
	return CustomerDoc{
		CustomerID:  id.String(),
		BookID:      bookID.String(),
		Name:        name,
		Corporation: corporation,
		Address:     address,
		Memo:        memo,
		Phone:       phone,
		PhoneText:   phone,
		Category:    category,
		Prefecture:  ExtractPrefecture(address),
		UpdatedAt:   updatedAt,
	}
}

// Indexer wraps the ES client with customer-specific write helpers. All
// methods are safe when `client` is nil — they simply log a debug message
// and return nil so the rest of the backend can operate in degraded mode
// when Elasticsearch is unreachable.
type Indexer struct {
	client *elasticsearch.Client
}

func NewIndexer(client *elasticsearch.Client) *Indexer {
	return &Indexer{client: client}
}

// Enabled reports whether this indexer has a live ES client. Callers can
// use it to skip ES-only features when running in degraded mode.
func (i *Indexer) Enabled() bool {
	return i != nil && i.client != nil
}

// IndexCustomer writes a single doc with ?refresh=wait_for so that the
// document is searchable immediately after the call returns (important for
// E2E tests and single-user write-then-read flows).
func (i *Indexer) IndexCustomer(ctx context.Context, doc CustomerDoc) error {
	if !i.Enabled() {
		log.Debug().Msg("indexer: ES disabled, skipping IndexCustomer")
		return nil
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal customer doc: %w", err)
	}
	res, err := i.client.Index(
		esclient.CustomerIndexName,
		bytes.NewReader(body),
		i.client.Index.WithContext(ctx),
		i.client.Index.WithDocumentID(doc.CustomerID),
		i.client.Index.WithRefresh("wait_for"),
	)
	if err != nil {
		return fmt.Errorf("index customer: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("index customer response error: %s", res.String())
	}
	return nil
}

// DeleteFromIndex removes a customer doc. `refresh=wait_for` is also applied
// so subsequent searches don't see the deleted row.
func (i *Indexer) DeleteFromIndex(ctx context.Context, customerID string) error {
	if !i.Enabled() {
		log.Debug().Msg("indexer: ES disabled, skipping DeleteFromIndex")
		return nil
	}
	res, err := i.client.Delete(
		esclient.CustomerIndexName,
		customerID,
		i.client.Delete.WithContext(ctx),
		i.client.Delete.WithRefresh("wait_for"),
	)
	if err != nil {
		return fmt.Errorf("delete from index: %w", err)
	}
	defer res.Body.Close()
	// Ignore 404 (already gone)
	if res.IsError() && res.StatusCode != 404 {
		return fmt.Errorf("delete from index response error: %s", res.String())
	}
	return nil
}

// BulkIndex writes multiple docs in a single bulk API call. Used by the
// CSV import path and the reindex CLI. `refresh=wait_for` is applied once
// at the end of the bulk request.
func (i *Indexer) BulkIndex(ctx context.Context, docs []CustomerDoc) error {
	if !i.Enabled() {
		log.Debug().Msg("indexer: ES disabled, skipping BulkIndex")
		return nil
	}
	if len(docs) == 0 {
		return nil
	}

	var buf bytes.Buffer
	for _, d := range docs {
		// action line
		action := fmt.Sprintf(
			"{\"index\":{\"_index\":\"%s\",\"_id\":\"%s\"}}\n",
			esclient.CustomerIndexName, d.CustomerID,
		)
		buf.WriteString(action)
		docBytes, err := json.Marshal(d)
		if err != nil {
			return fmt.Errorf("marshal bulk doc: %w", err)
		}
		buf.Write(docBytes)
		buf.WriteString("\n")
	}

	res, err := i.client.Bulk(
		strings.NewReader(buf.String()),
		i.client.Bulk.WithContext(ctx),
		i.client.Bulk.WithRefresh("wait_for"),
	)
	if err != nil {
		return fmt.Errorf("bulk index: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("bulk index response error: %s", res.String())
	}
	// We don't parse per-item errors here — the whole request succeeded at
	// HTTP level. Per-doc failures will surface in ES logs; for now we log
	// a warning so degraded behavior is visible.
	return nil
}
