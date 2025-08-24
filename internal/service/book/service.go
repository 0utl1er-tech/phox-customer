package book

import (
	"context"

	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
)

// Service bookサービスのインターフェース
type Service interface {
	GetBooksByUserID(ctx context.Context, req *connect.Request[customerv1.GetBooksByUserIDRequest]) (*connect.Response[customerv1.GetBooksByUserIDResponse], error)
	GetBookByID(ctx context.Context, req *connect.Request[customerv1.GetBookByIDRequest]) (*connect.Response[customerv1.GetBookByIDResponse], error)
	CreateBook(ctx context.Context, req *connect.Request[customerv1.CreateBookRequest]) (*connect.Response[customerv1.CreateBookResponse], error)
}

// ServiceImpl bookサービスの実装
type ServiceImpl struct {
	queries *db.Queries
}

// NewService bookサービスの新しいインスタンスを作成
func NewService(queries *db.Queries) Service {
	return &ServiceImpl{
		queries: queries,
	}
}
