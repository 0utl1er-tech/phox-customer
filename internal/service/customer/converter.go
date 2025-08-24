package customer

import (
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jinzhu/copier"
)

// convertCustomerToResponse DBのCustomerをレスポンス用のCustomerに変換（copier使用）
func convertCustomerToResponse(customer db.Customer) *customerv1.Customer {
	var response customerv1.Customer

	// copierを使って構造体をコピー
	if err := copier.Copy(&response, &customer); err != nil {
		// エラーが発生した場合は手動で変換
		return &customerv1.Customer{
			BookId:      customer.BookID.String(),
			Category:    "", // TODO: カテゴリ名を取得する処理を追加
			Name:        customer.Name,
			Corporation: customer.Corporation.String,
			Address:     customer.Address.String,
			Memo:        customer.Memo.String,
		}
	}

	// UUIDフィールドを文字列に変換
	response.BookId = customer.BookID.String()

	// オプショナルフィールドの処理
	if customer.Corporation.Valid {
		response.Corporation = customer.Corporation.String
	}
	if customer.Address.Valid {
		response.Address = customer.Address.String
	}
	if customer.Memo.Valid {
		response.Memo = customer.Memo.String
	}

	return &response
}

// convertCreateRequestToParams CreateCustomerRequestをCreateCustomerParamsに変換（copier使用）
func convertCreateRequestToParams(req *customerv1.CreateCustomerRequest, customerID, bookID, categoryID interface{}) db.CreateCustomerParams {
	var params db.CreateCustomerParams

	// copierを使って基本フィールドをコピー
	if err := copier.Copy(&params, req); err != nil {
		// エラーが発生した場合は手動で変換
		params = db.CreateCustomerParams{
			Name: req.Name,
		}
	}

	// IDフィールドを設定
	if id, ok := customerID.(string); ok {
		if uuid, err := parseUUID(id); err == nil {
			params.ID = uuid
		}
	}

	if bookUUID, ok := bookID.(string); ok {
		if uuid, err := parseUUID(bookUUID); err == nil {
			params.BookID = uuid
		}
	}

	// カテゴリIDを設定
	if catID, ok := categoryID.(pgtype.UUID); ok {
		params.CategoryID = catID
	}

	// オプショナルフィールドの処理
	if req.Corporation != "" {
		params.Corporation = pgtype.Text{String: req.Corporation, Valid: true}
	}
	if req.Address != "" {
		params.Address = pgtype.Text{String: req.Address, Valid: true}
	}
	if req.Memo != "" {
		params.Memo = pgtype.Text{String: req.Memo, Valid: true}
	}

	return params
}

// parseUUID 文字列をUUIDに変換するヘルパー関数
func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
