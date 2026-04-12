package search

import "testing"

func TestExtractPrefecture(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{"東京都 with city", "東京都渋谷区1-2-3", "東京都"},
		{"大阪府 with city", "大阪府大阪市北区4-5-6", "大阪府"},
		{"北海道 (ends in 道)", "北海道札幌市中央区13-14-15", "北海道"},
		{"神奈川県 (4-char prefix)", "神奈川県横浜市西区16-17-18", "神奈川県"},
		{"京都府 with district", "京都府京都市下京区19-20-21", "京都府"},
		{"兵庫県", "兵庫県神戸市中央区22-23-24", "兵庫県"},
		{"宮城県", "宮城県仙台市青葉区28-29-30", "宮城県"},
		{"和歌山県 (4-char prefix)", "和歌山県和歌山市1-2-3", "和歌山県"},
		{"鹿児島県 (4-char prefix)", "鹿児島県鹿児島市3-4-5", "鹿児島県"},

		// Edge cases
		{"empty string", "", ""},
		{"missing suffix", "東京", ""},
		{"missing suffix 神奈川", "神奈川市", ""},
		{"only prefecture name (no city)", "東京都", "東京都"},
		{"half-width number only", "1-2-3", ""},
		{"english address", "123 Main St", ""},
		{"prefecture in middle (must be prefix)", "xxxx東京都渋谷区", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractPrefecture(tc.address)
			if got != tc.want {
				t.Errorf("ExtractPrefecture(%q) = %q, want %q", tc.address, got, tc.want)
			}
		})
	}
}

func TestPrefecturesHas47Entries(t *testing.T) {
	if got := len(Prefectures()); got != 47 {
		t.Errorf("len(Prefectures()) = %d, want 47", got)
	}
}
