package customer

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// UpdateCustomer customerを更新
func (s *CustomerService) UpdateCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.UpdateCustomerRequest],
) (*connect.Response[customerv1.UpdateCustomerResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	customer, err := s.queries.GetCustomer(ctx, uuid.MustParse(req.Msg.Id))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの取得に失敗しました: %w", err))
	}

	permit, err := s.queries.GetPermitByBookIDAndUserID(ctx, db.GetPermitByBookIDAndUserIDParams{
		BookID: customer.BookID,
		UserID: userID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの取得に失敗しました: %w", err))
	}

	if permit.Role != db.RoleOwner && permit.Role != db.RoleEditor {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("customerの更新にはowner権限またはeditor権限が必要です"))
	}

	// nil安全にポインタの値を取得
	phoneText := pgtype.Text{Valid: false}
	if req.Msg.Phone != nil {
		phoneText = pgtype.Text{String: *req.Msg.Phone, Valid: true}
	}
	categoryText := pgtype.Text{Valid: false}
	if req.Msg.Category != nil {
		categoryText = pgtype.Text{String: *req.Msg.Category, Valid: true}
	}
	nameText := pgtype.Text{Valid: false}
	if req.Msg.Name != nil {
		nameText = pgtype.Text{String: *req.Msg.Name, Valid: true}
	}
	corporationText := pgtype.Text{Valid: false}
	if req.Msg.Corporation != nil {
		corporationText = pgtype.Text{String: *req.Msg.Corporation, Valid: true}
	}
	addressText := pgtype.Text{Valid: false}
	if req.Msg.Address != nil {
		addressText = pgtype.Text{String: *req.Msg.Address, Valid: true}
	}
	memoText := pgtype.Text{Valid: false}
	if req.Msg.Memo != nil {
		memoText = pgtype.Text{String: *req.Msg.Memo, Valid: true}
	}

	result, err := s.queries.UpdateCustomer(ctx, db.UpdateCustomerParams{
		ID:          customer.ID,
		Phone:       phoneText,
		Category:    categoryText,
		Name:        nameText,
		Corporation: corporationText,
		Address:     addressText,
		Memo:        memoText,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの更新に失敗しました: %w", err))
	}

	return connect.NewResponse(&customerv1.UpdateCustomerResponse{
		UpdatedCustomer: &customerv1.Customer{
			Id:          result.ID.String(),
			BookId:      result.BookID.String(),
			Phone:       result.Phone,
			Category:    result.Category,
			Name:        result.Name,
			Corporation: result.Corporation,
			Address:     result.Address,
			Memo:        result.Memo,
		},
	}), nil
}
