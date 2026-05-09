// Package activity は Activity (call / email_sent / email_received) の CRUD を
// 提供する Connect-Go サービス。既存の CallService を段階的に置き換える。
package activity

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/mail"
	"github.com/0utl1er-tech/phox-customer/internal/recording"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

// ActivityService は ActivityServiceHandler を実装する。
// mailClient / recordingSvc は nil 許容 (個別機能が有効な環境でのみ注入される)。
type ActivityService struct {
	queries      *db.Queries
	authorizer   *auth.Authorizer
	mailClient   *mail.SMTPClient
	recordingSvc *recording.Service
}

// NewActivityService は必要な依存を組み立てて ActivityService を返す。
// `mailClient` が nil の場合、`CreateActivityEmailSent` は CodeUnavailable を返す。
// `recordingSvc` が nil または disabled の場合、`GetActivityRecording` は CodeUnavailable を返す。
func NewActivityService(queries *db.Queries, mailClient *mail.SMTPClient, recordingSvc *recording.Service) *ActivityService {
	return &ActivityService{
		queries:      queries,
		authorizer:   auth.NewAuthorizer(queries),
		mailClient:   mailClient,
		recordingSvc: recordingSvc,
	}
}
