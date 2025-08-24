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
	// UUIDを生成
	customerID := uuid.New()

	// カテゴリIDを設定（オプショナル）
	var categoryID pgtype.UUID
	if req.Msg.CategoryId != "" {
		parsedUUID, err := uuid.Parse(req.Msg.CategoryId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		categoryID = pgtype.UUID{
			Bytes: parsedUUID,
			Valid: true,
		}
	}

	// データベースに保存
	customer, err := s.queries.CreateCustomer(ctx, db.CreateCustomerParams{
		ID:          customerID,
		BookID:      uuid.MustParse(req.Msg.BookId),
		CategoryID:  categoryID,
		Name:        req.Msg.Name,
		Corporation: pgtype.Text{String: req.Msg.Corporation, Valid: req.Msg.Corporation != ""},
		Address:     pgtype.Text{String: req.Msg.Address, Valid: req.Msg.Address != ""},
		Leader:      pgtype.UUID{Bytes: [16]byte{}, Valid: false}, // 後で実装
		Pic:         pgtype.UUID{Bytes: [16]byte{}, Valid: false}, // 後で実装
		Memo:        pgtype.Text{String: req.Msg.Memo, Valid: req.Msg.Memo != ""},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

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
