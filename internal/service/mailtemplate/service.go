// Package mailtemplate は Book に紐づくメールテンプレの CRUD Connect-Go
// ハンドラを提供する。
package mailtemplate

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

type MailTemplateService struct {
	queries    *db.Queries
	authorizer *auth.Authorizer
}

func NewMailTemplateService(queries *db.Queries) *MailTemplateService {
	return &MailTemplateService{
		queries:    queries,
		authorizer: auth.NewAuthorizer(queries),
	}
}
