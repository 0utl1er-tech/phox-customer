package customer

import (
	"context"
	"fmt"

	callv1 "github.com/0utl1er-tech/phox-customer/gen/pb/call/v1"
	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	staffv1 "github.com/0utl1er-tech/phox-customer/gen/pb/staff/v1"
	userv1 "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
)

func (s *CustomerService) GetCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.GetCustomerRequest],
) (*connect.Response[customerv1.GetCustomerResponse], error) {
	userID := req.Header().Get("X-User-ID")
	if userID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("X-User-IDがヘッダーに見つかりません"))
	}
	customer, err := s.queries.GetCustomer(ctx, uuid.MustParse(req.Msg.Id))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("customerの取得に失敗しました: %w", err))
	}

	_, err = s.queries.GetBookByIDAndUserID(ctx, db.GetBookByIDAndUserIDParams{
		ID:     customer.BookID,
		UserID: userID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("このbookにアクセスする権限がありません: %w", err))
	}

	callRows, err := s.queries.ListCalls(ctx, customer.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("callsの取得に失敗しました: %w", err))
	}

	calls := make([]*callv1.Call, len(callRows))
	for i, call := range callRows {
		calls[i] = &callv1.Call{
			Id: call.ID.String(),
			Contact: &contactv1.Contact{
				Id:    call.ContactID.String(),
				Phone: call.ContactPhone,
				Mail:  call.ContactMail,
				Fax:   call.ContactFax,
			},
			User: &userv1.User{
				Id:   call.UserID,
				Name: call.UserName,
			},
		}
	}

	return connect.NewResponse(&customerv1.GetCustomerResponse{
		Customer: &customerv1.Customer{
			Id:          customer.ID.String(),
			BookId:      customer.BookID.String(),
			Category:    customer.Category,
			Name:        customer.Name,
			Corporation: customer.Corporation,
			Address:     customer.Address,
			Memo:        customer.Memo,
			Pic: &staffv1.Staff{
				Id:   customer.PicID.String(),
				Name: customer.PicName,
				Sex:  customer.PicSex,
			},
			Leader: &staffv1.Staff{
				Id:   customer.LeaderID.String(),
				Name: customer.LeaderName,
				Sex:  customer.LeaderSex,
			},
			Calls: calls,
		},
	}), nil
}
