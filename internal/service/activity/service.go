// Package activity は Activity (call / email_sent / email_received) の CRUD を
// 提供する Connect-Go サービス。既存の CallService を段階的に置き換える。
package activity

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/mail"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

// ActivityService は ActivityServiceHandler を実装する。
// mailClient は nil 許容 (Phase 11 以降で注入される / SMTP が無効な環境では nil)。
type ActivityService struct {
	queries    *db.Queries
	authorizer *auth.Authorizer
	mailClient *mail.SMTPClient
}

// NewActivityService は必要な依存を組み立てて ActivityService を返す。
// `mailClient` が nil の場合、`CreateActivityEmailSent` は CodeUnavailable を返す。
func NewActivityService(queries *db.Queries, mailClient *mail.SMTPClient) *ActivityService {
	return &ActivityService{
		queries:    queries,
		authorizer: auth.NewAuthorizer(queries),
		mailClient: mailClient,
	}
}
