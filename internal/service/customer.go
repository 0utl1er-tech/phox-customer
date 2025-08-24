package service

import (
	"context"

	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type CustomerService struct {
	queries *db.Queries
}

func NewCustomerService(queries *db.Queries) *CustomerService {
	return &CustomerService{
		queries: queries,
	}
}

func (s *CustomerService) CreateCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.CreateCustomerRequest],
) (*connect.Response[customerv1.CreateCustomerResponse], error) {
	// 認証済みユーザーのIDを取得
	userID, err := RequireUserID(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	// UUIDを生成
	customerID := uuid.New()
	bookID := uuid.MustParse(req.Msg.BookId)

	// カテゴリIDを設定（オプショナル）
	var categoryID pgtype.UUID
	if req.Msg.Category != "" {
		// カテゴリのUpsert処理
		category, err := s.queries.UpsertCategory(ctx, db.UpsertCategoryParams{
			ID:     uuid.New(),
			BookID: bookID,
			Name:   req.Msg.Category,
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		categoryID = pgtype.UUID{
			Bytes: category.ID,
			Valid: true,
		}
	}

	// データベースに保存
	customer, err := s.queries.CreateCustomer(ctx, db.CreateCustomerParams{
		ID:          customerID,
		BookID:      bookID,
		CategoryID:  categoryID,
		Name:        req.Msg.Name,
		Corporation: pgtype.Text{String: req.Msg.Corporation, Valid: req.Msg.Corporation != ""},
		Address:     pgtype.Text{String: req.Msg.Address, Valid: req.Msg.Address != ""},
		Memo:        pgtype.Text{String: req.Msg.Memo, Valid: req.Msg.Memo != ""},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// ログ出力例（user_idを含む）
	// log.Printf("Customer created by user %s: %s", userID, customer.Name)
	_ = userID // 認証済みユーザーIDを取得済み

	return connect.NewResponse(&customerv1.CreateCustomerResponse{
		Id:     customer.ID.String(),
		BookId: customer.BookID.String(),
		Name:   customer.Name,
	}), nil
}

func (s *CustomerService) SearchCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.SearchCustomerRequest],
) (*connect.Response[customerv1.SearchCustomerResponse], error) {
	// TODO: 実際の実装を追加
	return connect.NewResponse(&customerv1.SearchCustomerResponse{
		Customers: []*customerv1.Customer{},
	}), nil
}

func (s *CustomerService) UpdateCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.UpdateCustomerRequest],
) (*connect.Response[customerv1.UpdateCustomerResponse], error) {
	// TODO: 実際の実装を追加
	name := ""
	if req.Msg.Name != nil {
		name = *req.Msg.Name
	}
	return connect.NewResponse(&customerv1.UpdateCustomerResponse{
		Id:     "dummy-id",
		BookId: req.Msg.BookId,
		Name:   name,
	}), nil
}

func (s *CustomerService) DeleteCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.DeleteCustomerRequest],
) (*connect.Response[customerv1.DeleteCustomerResponse], error) {
	// TODO: 実際の実装を追加
	return connect.NewResponse(&customerv1.DeleteCustomerResponse{
		CustomerId: req.Msg.CustomerId,
	}), nil
}
