package customer

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/customfields"
	"github.com/0utl1er-tech/phox-customer/internal/util"
)

// UpdateCustomer customerを更新
func (s *CustomerService) UpdateCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.UpdateCustomerRequest],
) (*connect.Response[customerv1.UpdateCustomerResponse], error) {
	id, err := util.ParseUUID("id", req.Msg.Id)
	if err != nil {
		return nil, err
	}

	customer, err := s.queries.GetCustomer(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの取得に失敗しました: %w", err))
	}

	if err := s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleEditor); err != nil {
		return nil, err
	}

	// Phase 29b: custom_fields は未指定 (nil) なら現状維持、指定時は全置換。
	// COALESCE(narg, custom_fields) なので nil を渡せば既存値が残る。
	var cf []byte
	if req.Msg.CustomFields != nil {
		cf, err = customfields.MarshalSanitized(req.Msg.CustomFields.Values)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("custom_fields: %w", err))
		}
	}

	// 空文字列は「未指定」として既存値を保持する (util.OptionalText の doc 参照)
	result, err := s.queries.UpdateCustomer(ctx, db.UpdateCustomerParams{
		ID:           customer.ID,
		Phone:        util.OptionalText(req.Msg.Phone),
		Category:     util.OptionalText(req.Msg.Category),
		Name:         util.OptionalText(req.Msg.Name),
		Corporation:  util.OptionalText(req.Msg.Corporation),
		Address:      util.OptionalText(req.Msg.Address),
		Memo:         util.OptionalText(req.Msg.Memo),
		Mail:         util.OptionalText(req.Msg.Mail),
		CustomFields: cf,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの更新に失敗しました: %w", err))
	}

	s.indexCustomer(ctx, result, "updated")

	return connect.NewResponse(&customerv1.UpdateCustomerResponse{
		UpdatedCustomer: modelToProto(result),
	}), nil
}
