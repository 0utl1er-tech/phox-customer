// Package icalfeed は iCalendar 購読 URL のトークン CRUD を扱う Connect サービス。
// 実際の feed 配信は internal/ical.Handler が HTTP レベルでやる。
package icalfeed

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

// ICalFeedService は Keycloak 認証済みユーザーが自分の feed トークンを
// 生成・再生成・削除できるようにする。
type ICalFeedService struct {
	queries         *db.Queries
	icalFeedBaseURL string // 例: http://localhost:8082 — purchase URL のベース
}

func NewICalFeedService(queries *db.Queries, icalFeedBaseURL string) *ICalFeedService {
	return &ICalFeedService{
		queries:         queries,
		icalFeedBaseURL: icalFeedBaseURL,
	}
}
