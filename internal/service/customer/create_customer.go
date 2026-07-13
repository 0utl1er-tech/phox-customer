package customer

import (
	"context"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
)

// CreateCustomer 新しいcustomerを作成
//
// 注意: 以前はこの RPC に permit チェックが無く、任意のユーザーが任意の Book に
// Customer を追加できる状態だった。owner/editor 権限を要求するように修正済み。
func (s *CustomerService) CreateCustomer(
	ctx context.Context,
	req *connect.Request[customerv1.CreateCustomerRequest],
) (*connect.Response[customerv1.CreateCustomerResponse], error) {
	bookID, err := util.ParseUUID("book_id", req.Msg.BookId)
	if err != nil {
		return nil, err
	}

	if err := s.authorizer.CheckPermission(ctx, bookID, db.RoleEditor); err != nil {
		return nil, err
	}

	customer, err := s.queries.CreateCustomer(ctx, db.CreateCustomerParams{
		ID:          uuid.New(),
		BookID:      bookID,
		Phone:       req.Msg.Phone,
		Category:    req.Msg.Category,
		Name:        req.Msg.Name,
		Corporation: req.Msg.Corporation,
		Address:     req.Msg.Address,
		Memo:        req.Msg.Memo,
		Mail:        req.Msg.Mail,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.indexCustomer(ctx, customer, "created")

	// Phase 26: 既に取込済みのメール (MailboxMessage) のうち、この顧客の mail に
	// 一致する未紐付けのものを遡って Activity 化し、タイムラインに載せる。
	// メール → 顧客登録 の順で作業しても過去メールが履歴に出るようにする。
	BackfillCustomerMail(ctx, s.queries, customer.ID, customer.Mail)

	return connect.NewResponse(&customerv1.CreateCustomerResponse{
		Customer: modelToProto(customer),
	}), nil
}

// BackfillMailboxTimeline は既存顧客に対しメール履歴を紐付ける (editor 必須)。
// create_customer の upsert 経路 (既存顧客が返る場合) から呼ぶ。冪等。
func (s *CustomerService) BackfillMailboxTimeline(ctx context.Context, bookID, customerID uuid.UUID, mail string) error {
	if err := s.authorizer.CheckPermission(ctx, bookID, db.RoleEditor); err != nil {
		return err
	}
	BackfillCustomerMail(ctx, s.queries, customerID, mail)
	return nil
}

// BackfillCustomerMail は customer の mail に一致する未紐付け MailboxMessage を
// Activity 化する (best-effort、失敗しても顧客作成は成功扱い)。冪等なので
// create_customer の upsert 経路など何度呼んでも安全。
func BackfillCustomerMail(ctx context.Context, q *db.Queries, customerID uuid.UUID, mail string) {
	backfillMail(ctx, q, customerID, pgtype.UUID{}, mail)
}

// BackfillContactMail は contact の mail に一致する未紐付けメールを、その顧客の
// タイムラインに Activity 化する (contact_id 付き)。複数アドレスを持つ取引先を
// contact として束ねると、各アドレスの履歴が顧客に集約される。
func BackfillContactMail(ctx context.Context, q *db.Queries, customerID, contactID uuid.UUID, mail string) {
	backfillMail(ctx, q, customerID, pgtype.UUID{Bytes: contactID, Valid: true}, mail)
}

func backfillMail(ctx context.Context, q *db.Queries, customerID uuid.UUID, contactID pgtype.UUID, mail string) {
	if mail == "" {
		return
	}
	n, err := q.BackfillActivitiesForCustomerEmail(ctx, db.BackfillActivitiesForCustomerEmailParams{
		Email:      mail,
		CustomerID: pgtype.UUID{Bytes: customerID, Valid: true},
		ContactID:  contactID,
	})
	if err != nil {
		log.Warn().Err(err).Str("mail", mail).Str("customer_id", customerID.String()).
			Msg("customer: mailbox activity backfill failed")
		return
	}
	if n > 0 {
		log.Info().Int64("linked", n).Str("mail", mail).Str("customer_id", customerID.String()).
			Msg("customer: backfilled mailbox messages into timeline")
	}
}
