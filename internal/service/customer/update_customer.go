package customer

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/search"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
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

	// BUG FIX (2026-04-21):
	//   Previously `req.Msg.Phone != nil` was enough to mark Valid=true, so
	//   an empty-string payload from the UI ({"phone":""}) passed through
	//   as pgtype.Text{String:"", Valid:true}. The SQL uses COALESCE($1,
	//   existing) which treats empty string as a legitimate value (not
	//   NULL), so DB values were silently overwritten to "".
	//
	//   Treat empty strings as "unset" (Valid=false). UI sends only the
	//   fields it actually wants to change. Explicit clearing of a field
	//   would require a dedicated RPC / sentinel value, which we don't
	//   support today and isn't in the product.
	toText := func(p *string) pgtype.Text {
		if p == nil || *p == "" {
			return pgtype.Text{Valid: false}
		}
		return pgtype.Text{String: *p, Valid: true}
	}
	phoneText := toText(req.Msg.Phone)
	categoryText := toText(req.Msg.Category)
	nameText := toText(req.Msg.Name)
	corporationText := toText(req.Msg.Corporation)
	addressText := toText(req.Msg.Address)
	memoText := toText(req.Msg.Memo)
	mailText := toText(req.Msg.Mail)

	result, err := s.queries.UpdateCustomer(ctx, db.UpdateCustomerParams{
		ID:          customer.ID,
		Phone:       phoneText,
		Category:    categoryText,
		Name:        nameText,
		Corporation: corporationText,
		Address:     addressText,
		Memo:        memoText,
		Mail:        mailText,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの更新に失敗しました: %w", err))
	}

	// Write-after-commit ES index (idempotent: 同じ id に re-index するだけ)。
	if idxErr := s.indexer.IndexCustomer(ctx, search.NewCustomerDoc(
		result.ID,
		result.BookID,
		result.Name,
		result.Corporation,
		result.Address,
		result.Memo,
		result.Phone,
		result.Category,
		result.UpdatedAt,
	)); idxErr != nil {
		log.Warn().Err(idxErr).Str("customer_id", result.ID.String()).Msg("failed to reindex updated customer")
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
			Mail:        result.Mail,
		},
	}), nil
}
