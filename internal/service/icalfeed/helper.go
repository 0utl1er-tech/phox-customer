package icalfeed

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	icalfeedv1 "github.com/0utl1er-tech/phox-customer/gen/pb/icalfeed/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// newToken は URL-safe base64 (32 byte) のトークンを生成する。
// 結果長: 43 char (padding 無し)。crypto/rand 由来なのでエントロピー十分。
func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("icalfeed: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// buildFeedURL は保存された token から購読 URL を組み立てる。
// スキームは `webcal://` に書き換える (1999 年来の Apple 慣習で、
// calendar feed 用の universal subscribe link として全主要カレンダーが対応):
//   - ブラウザで webcal:// をクリック → OS が Calendar.app などを自動起動
//   - カレンダーアプリに貼り付け → webcal:// を内部で http(s):// に置換して GET
//
// backend 自体は http/https で listen しているので、scheme を webcal に差し替える
// のは購読リンクを返す側 (この関数) の責務。
func buildFeedURL(base, token string) string {
	trimmed := strings.TrimRight(base, "/")
	// `http://` / `https://` のスキームを剥がして `webcal://` に置換
	noScheme := trimmed
	if strings.HasPrefix(noScheme, "https://") {
		noScheme = strings.TrimPrefix(noScheme, "https://")
	} else if strings.HasPrefix(noScheme, "http://") {
		noScheme = strings.TrimPrefix(noScheme, "http://")
	}
	return fmt.Sprintf("webcal://%s/ical/%s.ics", noScheme, token)
}

// modelToProto は DB 行を proto message に変換する。
func modelToProto(row db.UserICalFeed, base string) *icalfeedv1.ICalFeedInfo {
	return &icalfeedv1.ICalFeedInfo{
		Url:       buildFeedURL(base, row.Token),
		CreatedAt: timestamppb.New(row.CreatedAt),
		UpdatedAt: timestamppb.New(row.UpdatedAt),
	}
}
