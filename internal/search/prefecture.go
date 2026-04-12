// Package search provides helpers and the Elasticsearch-backed indexer/search
// service for phox-customer.
package search

import "strings"

// prefectures lists all 47 Japanese prefectures.
// IMPORTANT: order must be longest-first so that `神奈川県` (4 chars) is matched
// before any shorter prefix would erroneously match. `北海道` is special —
// it ends in `道` rather than `県/都/府`, which is why the extraction uses
// explicit full-name prefix matching instead of regex suffix matching.
//
// If this list is edited, keep `phox-ui/src/lib/prefectures.ts` in sync
// (the UI mirrors the same 47 for the SearchSidebar dropdown).
var prefectures = []string{
	// 4 chars first
	"神奈川県",
	"和歌山県",
	"鹿児島県",
	// 3 chars
	"北海道",
	"青森県",
	"岩手県",
	"宮城県",
	"秋田県",
	"山形県",
	"福島県",
	"茨城県",
	"栃木県",
	"群馬県",
	"埼玉県",
	"千葉県",
	"東京都",
	"新潟県",
	"富山県",
	"石川県",
	"福井県",
	"山梨県",
	"長野県",
	"岐阜県",
	"静岡県",
	"愛知県",
	"三重県",
	"滋賀県",
	"京都府",
	"大阪府",
	"兵庫県",
	"奈良県",
	"鳥取県",
	"島根県",
	"岡山県",
	"広島県",
	"山口県",
	"徳島県",
	"香川県",
	"愛媛県",
	"高知県",
	"福岡県",
	"佐賀県",
	"長崎県",
	"熊本県",
	"大分県",
	"宮崎県",
	"沖縄県",
}

// Prefectures returns a copy of the 47-prefecture slice (longest-first order).
// Callers should not mutate the result.
func Prefectures() []string {
	out := make([]string, len(prefectures))
	copy(out, prefectures)
	return out
}

// ExtractPrefecture returns the prefecture name found at the start of the
// given address string, or the empty string if no prefecture prefix matches.
// Matching is case-sensitive and requires the full "県/都/府/道" suffix so
// that `神奈川市` does not accidentally match `神奈川県`.
func ExtractPrefecture(address string) string {
	for _, p := range prefectures {
		if strings.HasPrefix(address, p) {
			return p
		}
	}
	return ""
}
