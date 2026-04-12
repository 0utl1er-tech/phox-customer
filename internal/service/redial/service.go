// Package redial は掛け直し予定の CRUD と Google Calendar 連携を扱う。
package redial

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/gcal"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

type RedialService struct {
	queries    *db.Queries
	authorizer *auth.Authorizer
	gcalClient gcal.Client
}

func NewRedialService(queries *db.Queries, gcalClient gcal.Client) *RedialService {
	return &RedialService{
		queries:    queries,
		authorizer: auth.NewAuthorizer(queries),
		gcalClient: gcalClient,
	}
}
