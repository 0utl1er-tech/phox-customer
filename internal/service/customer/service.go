package customer

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

// ServiceImpl customerサービスの実装
type CustomerService struct {
	queries *db.Queries
}

// NewService customerサービスの新しいインスタンスを作成
func NewCustomerService(queries *db.Queries) *CustomerService {
	return &CustomerService{
		queries: queries,
	}
}
