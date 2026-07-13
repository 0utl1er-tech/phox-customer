package customer

import (
	"context"

	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/search"
	"github.com/rs/zerolog/log"
)

// contactsToProto は Contact 行の slice を proto に変換する。
func contactsToProto(cs []db.Contact) []*contactv1.Contact {
	out := make([]*contactv1.Contact, 0, len(cs))
	for _, c := range cs {
		out = append(out, &contactv1.Contact{
			Id:         c.ID.String(),
			CustomerId: c.CustomerID.String(),
			Name:       c.Name,
			Sex:        c.Sex,
			Phone:      c.Phone,
			Mail:       c.Mail,
			Fax:        c.Fax,
		})
	}
	return out
}

// modelToProto converts a db.Customer row to its proto representation.
//
// sqlc はクエリごとに同一カラム構成でも別の row 型を生成する (フィールド順が
// 異なり struct 変換も使えない) ため、GetCustomerRow / ListCustomersRow 用の
// 変換も併せて用意している。proto にフィールドを足すときは 3 つとも更新する。
func modelToProto(c db.Customer) *customerv1.Customer {
	return &customerv1.Customer{
		Id:          c.ID.String(),
		BookId:      c.BookID.String(),
		Phone:       c.Phone,
		Category:    c.Category,
		Name:        c.Name,
		Corporation: c.Corporation,
		Address:     c.Address,
		Memo:        c.Memo,
		Mail:        c.Mail,
	}
}

func getRowToProto(c db.GetCustomerRow) *customerv1.Customer {
	return &customerv1.Customer{
		Id:          c.ID.String(),
		BookId:      c.BookID.String(),
		Phone:       c.Phone,
		Category:    c.Category,
		Name:        c.Name,
		Corporation: c.Corporation,
		Address:     c.Address,
		Memo:        c.Memo,
		Mail:        c.Mail,
	}
}

func listRowToProto(c db.ListCustomersRow) *customerv1.Customer {
	return &customerv1.Customer{
		Id:          c.ID.String(),
		BookId:      c.BookID.String(),
		Phone:       c.Phone,
		Category:    c.Category,
		Name:        c.Name,
		Corporation: c.Corporation,
		Address:     c.Address,
		Memo:        c.Memo,
		Mail:        c.Mail,
	}
}

// indexCustomer は write-after-commit で ES に customer を反映する。
// 失敗しても DB の成功は返す (degraded mode) ため、エラーは warn ログに
// 落とすだけで呼び出し元へは返さない。idempotent (同じ id へ re-index)。
func (s *CustomerService) indexCustomer(ctx context.Context, c db.Customer, action string) {
	if err := s.indexer.IndexCustomer(ctx, search.NewCustomerDoc(
		c.ID,
		c.BookID,
		c.Name,
		c.Corporation,
		c.Address,
		c.Memo,
		c.Phone,
		c.Category,
		c.UpdatedAt,
	)); err != nil {
		log.Warn().Err(err).Str("customer_id", c.ID.String()).Msgf("failed to index %s customer", action)
	}
}
