package customer_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/customer"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// create_customer 時、mail 一致の既存 MailboxMessage が Activity 化されて
// 顧客タイムラインに載ることを検証 (Phase 26 バックフィル)。
func TestCreateCustomer_BackfillsMailboxTimeline(t *testing.T) {
	_, q := testutil.SetupTestDB(t)
	ctx := context.Background()
	cid := testutil.TestCompanyID(t, q)
	owner := testutil.TestUser(t, q, "bf-owner-"+uuid.NewString(), cid)
	bk := testutil.TestBook(t, q, owner.ID)

	// メールボックス + 未紐付けメール (INBOX 受信 / Sent 送信) を seed。
	mbID := uuid.New()
	_, err := q.CreateMailbox(ctx, db.CreateMailboxParams{
		ID: mbID, CompanyID: cid, Address: "mb-" + mbID.String() + "@0utl1er.tech",
		SmtpUsername: "mb@0utl1er.tech", PasswordEnc: []byte("x"), Active: true,
	})
	require.NoError(t, err)

	prospect := "prospect-" + uuid.NewString()[:8] + "@example.com"
	recvID := "<" + uuid.NewString() + "@example.com>"
	sentID := "<" + uuid.NewString() + "@example.com>"
	_, err = q.CreateMailboxMessage(ctx, db.CreateMailboxMessageParams{
		ID: uuid.New(), MailboxID: mbID, Folder: "INBOX", MessageID: recvID,
		FromAddr: prospect, ToAddrs: "mb@0utl1er.tech", Subject: "お問い合わせ",
		BodyText: "はじめまして", OccurredAt: time.Now().Add(-48 * time.Hour),
	})
	require.NoError(t, err)
	_, err = q.CreateMailboxMessage(ctx, db.CreateMailboxMessageParams{
		ID: uuid.New(), MailboxID: mbID, Folder: "Sent", MessageID: sentID,
		FromAddr: "mb@0utl1er.tech", ToAddrs: "someone@x.com, " + prospect, Subject: "Re: お問い合わせ",
		BodyText: "ご連絡ありがとうございます", OccurredAt: time.Now().Add(-24 * time.Hour),
	})
	require.NoError(t, err)

	svc := customer.NewCustomerService(q, nil)
	ctxAuth := testutil.AuthContext(t, owner.ID, "owner@test.com")
	resp, err := svc.CreateCustomer(ctxAuth, connect.NewRequest(&customerv1.CreateCustomerRequest{
		BookId: bk.ID.String(), Name: "見込み客", Mail: prospect,
	}))
	require.NoError(t, err)
	custID, _ := uuid.Parse(resp.Msg.Customer.Id)

	// 受信・送信の両方が Activity 化される。
	recv, err := q.GetActivityByMessageID(ctx, pgtype.Text{String: recvID, Valid: true})
	require.NoError(t, err, "受信メールが履歴に載るべき")
	assert.Equal(t, custID, recv.CustomerID)
	assert.Equal(t, "email_received", recv.Type)

	sent, err := q.GetActivityByMessageID(ctx, pgtype.Text{String: sentID, Valid: true})
	require.NoError(t, err, "送信メール (To に含む) も履歴に載るべき")
	assert.Equal(t, "email_sent", sent.Type)

	// MailboxMessage 側も customer_id が紐付く。
	msgs, err := q.ListMailboxMessages(ctx, db.ListMailboxMessagesParams{MailboxID: mbID, Limit: 10})
	require.NoError(t, err)
	for _, m := range msgs {
		assert.True(t, m.CustomerID.Valid, "メールが顧客に紐付くべき: %s", m.Subject)
	}

	// 冪等: BackfillMailboxTimeline を再実行しても重複 Activity は増えない。
	require.NoError(t, svc.BackfillMailboxTimeline(ctxAuth, bk.ID, custID, prospect))
	// GetActivityByMessageID は :one なので重複してれば別途壊れる。
	_, err = q.GetActivityByMessageID(ctx, pgtype.Text{String: recvID, Valid: true})
	require.NoError(t, err)
}
