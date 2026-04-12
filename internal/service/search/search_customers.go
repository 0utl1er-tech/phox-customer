package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect"
	searchv1 "github.com/0utl1er-tech/phox-customer/gen/pb/search/v1"
	"github.com/0utl1er-tech/phox-customer/internal/search/esclient"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

// SearchCustomers は Elasticsearch に対して full-text + facet 検索を行い、
// ユーザーが権限を持つ Book 内の Customer のみを返す。
func (s *SearchService) SearchCustomers(
	ctx context.Context,
	req *connect.Request[searchv1.SearchCustomersRequest],
) (*connect.Response[searchv1.SearchCustomersResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	if s.es == nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("search service is unavailable (Elasticsearch not configured)"))
	}

	// 1) 認可: ユーザーがアクセスできる Book を DB から取得
	permits, err := s.queries.GetPermitsByUserID(ctx, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to load permits: %w", err))
	}
	userBookIDs := make([]string, 0, len(permits))
	for _, p := range permits {
		userBookIDs = append(userBookIDs, p.BookID.String())
	}
	if len(userBookIDs) == 0 {
		// 権限 Book なしなら検索結果も 0 件で即 return
		return connect.NewResponse(&searchv1.SearchCustomersResponse{
			Hits:             []*searchv1.SearchHit{},
			Total:            0,
			PrefectureFacets: []*searchv1.PrefectureFacet{},
		}), nil
	}

	// 2) request の book_ids と userBookIDs の積集合を取る
	allowedBookIDs := userBookIDs
	if len(req.Msg.BookIds) > 0 {
		allowed := make(map[string]struct{}, len(userBookIDs))
		for _, id := range userBookIDs {
			allowed[id] = struct{}{}
		}
		intersection := make([]string, 0, len(req.Msg.BookIds))
		for _, id := range req.Msg.BookIds {
			if _, ok := allowed[id]; ok {
				intersection = append(intersection, id)
			}
		}
		allowedBookIDs = intersection
		if len(allowedBookIDs) == 0 {
			return connect.NewResponse(&searchv1.SearchCustomersResponse{
				Hits:             []*searchv1.SearchHit{},
				Total:            0,
				PrefectureFacets: []*searchv1.PrefectureFacet{},
			}), nil
		}
	}

	// 3) ES クエリ組み立て
	limit := int(req.Msg.Limit)
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := int(req.Msg.Offset)
	if offset < 0 {
		offset = 0
	}

	query := buildSearchQuery(req.Msg.Query, allowedBookIDs, req.Msg.Prefecture, limit, offset)
	queryBytes, err := json.Marshal(query)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("marshal query: %w", err))
	}

	res, err := s.es.Search(
		s.es.Search.WithContext(ctx),
		s.es.Search.WithIndex(esclient.CustomerIndexName),
		s.es.Search.WithBody(bytes.NewReader(queryBytes)),
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("es search: %w", err))
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("es search error: %s", res.String()))
	}

	// 4) レスポンスを parse
	var esResp esSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&esResp); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("decode es response: %w", err))
	}

	hits := make([]*searchv1.SearchHit, 0, len(esResp.Hits.Hits))
	for _, h := range esResp.Hits.Hits {
		hits = append(hits, &searchv1.SearchHit{
			CustomerId:  h.Source.CustomerID,
			BookId:      h.Source.BookID,
			Name:        h.Source.Name,
			Corporation: h.Source.Corporation,
			Address:     h.Source.Address,
			Phone:       h.Source.Phone,
			Prefecture:  h.Source.Prefecture,
			Score:       h.Score,
		})
	}

	facets := make([]*searchv1.PrefectureFacet, 0, len(esResp.Aggregations.Prefectures.Buckets))
	for _, b := range esResp.Aggregations.Prefectures.Buckets {
		facets = append(facets, &searchv1.PrefectureFacet{
			Prefecture: b.Key,
			Count:      b.DocCount,
		})
	}

	return connect.NewResponse(&searchv1.SearchCustomersResponse{
		Hits:             hits,
		Total:            esResp.Hits.Total.Value,
		PrefectureFacets: facets,
	}), nil
}

// buildSearchQuery は ES の Query DSL を map ベースで組み立てる。
// 生の map を json.Marshal する方式で、文字列テンプレートより安全 (エスケープ漏れ防止)。
func buildSearchQuery(
	query string,
	bookIDs []string,
	prefecture string,
	limit, offset int,
) map[string]any {
	filters := []any{
		map[string]any{"terms": map[string]any{"book_id": bookIDs}},
	}
	if prefecture != "" {
		filters = append(filters, map[string]any{
			"term": map[string]any{"prefecture": prefecture},
		})
	}

	boolQuery := map[string]any{
		"filter": filters,
	}
	if query != "" {
		boolQuery["must"] = []any{
			map[string]any{
				"multi_match": map[string]any{
					"query":  query,
					"fields": []string{"name^3", "corporation^2", "address", "memo", "phone_text", "phone"},
				},
			},
		}
	} else {
		boolQuery["must"] = []any{map[string]any{"match_all": map[string]any{}}}
	}

	return map[string]any{
		"from": offset,
		"size": limit,
		"query": map[string]any{
			"bool": boolQuery,
		},
		"aggs": map[string]any{
			"prefectures": map[string]any{
				"terms": map[string]any{
					"field": "prefecture",
					"size":  50,
				},
			},
		},
	}
}

// --- ES レスポンス schema (必要最小限) ---

type esSearchResponse struct {
	Hits struct {
		Total struct {
			Value int64 `json:"value"`
		} `json:"total"`
		Hits []struct {
			ID     string  `json:"_id"`
			Score  float64 `json:"_score"`
			Source struct {
				CustomerID  string `json:"customer_id"`
				BookID      string `json:"book_id"`
				Name        string `json:"name"`
				Corporation string `json:"corporation"`
				Address     string `json:"address"`
				Phone       string `json:"phone"`
				Prefecture  string `json:"prefecture"`
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
	Aggregations struct {
		Prefectures struct {
			Buckets []struct {
				Key      string `json:"key"`
				DocCount int64  `json:"doc_count"`
			} `json:"buckets"`
		} `json:"prefectures"`
	} `json:"aggregations"`
}
