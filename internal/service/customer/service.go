package customer

import (
	"context"

	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
)

// Service customerサービスのインターフェース
type Service interface {
	CreateCustomer(ctx context.Context, req *connect.Request[customerv1.CreateCustomerRequest]) (*connect.Response[customerv1.CreateCustomerResponse], error)
	SearchCustomer(ctx context.Context, req *connect.Request[customerv1.SearchCustomerRequest]) (*connect.Response[customerv1.SearchCustomerResponse], error)
	UpdateCustomer(ctx context.Context, req *connect.Request[customerv1.UpdateCustomerRequest]) (*connect.Response[customerv1.UpdateCustomerResponse], error)
	DeleteCustomer(ctx context.Context, req *connect.Request[customerv1.DeleteCustomerRequest]) (*connect.Response[customerv1.DeleteCustomerResponse], error)
}

// ServiceImpl customerサービスの実装
type ServiceImpl struct {
	queries *db.Queries
}

// NewService customerサービスの新しいインスタンスを作成
func NewService(queries *db.Queries) Service {
	return &ServiceImpl{
		queries: queries,
	}
}
