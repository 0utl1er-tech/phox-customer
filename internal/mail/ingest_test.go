package mail

import (
	"context"
	"testing"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ingestMessages が受信メールを該当顧客の Activity に変換し、mailbox_id を
// 記録することを検証 (Phase 25/C の取込みロジック本体)。
func TestIngestMessages_StampsMailboxID(t *testing.T) {
	_, q := testutil.SetupTestDB(t)
	ctx := context.Background()
	cid := testutil.TestCompanyID(t, q)
	owner := testutil.TestUser(t, q, "ingest-user-"+uuid.NewString(), cid)
	bk := testutil.TestBook(t, q, owner.ID)

	custMail := "cust-" + uuid.NewString()[:8] + "@example.com"
	cust, err := q.CreateCustomer(ctx, db.CreateCustomerParams{
		ID: uuid.New(), BookID: bk.ID, Name: "取込テスト顧客", Phone: "03-0000-0000", Mail: custMail,
	})
	require.NoError(t, err)

	mbID := uuid.New()
	enc := []byte("x")
	_, err = q.CreateMailbox(ctx, db.CreateMailboxParams{
		ID: mbID, CompanyID: cid, Address: "mb-" + mbID.String() + "@0utl1er.tech",
		SmtpUsername: "mb@0utl1er.tech", PasswordEnc: enc, Active: true,
	})
	require.NoError(t, err)

	msgID := "<" + uuid.NewString() + "@example.com>"
	msgs := []ParsedMessage{{
		MessageID: msgID,
		Date:      time.Now(),
		Subject:   "顧客からの返信",
		From:      custMail, // 顧客からの受信
		To:        []string{"mb@0utl1er.tech"},
	}}

	ingestMessages(ctx, q, msgs, "email_received", "system", pgtype.UUID{Bytes: mbID, Valid: true})

	// Activity が作られ、customer/type/mailbox_id が正しい。
	act, err := q.GetActivityByMessageID(ctx, pgtype.Text{String: msgID, Valid: true})
	require.NoError(t, err, "受信メールが Activity として取込まれるべき")
	assert.Equal(t, cust.ID, act.CustomerID)
	assert.Equal(t, "email_received", act.Type)
	require.True(t, act.MailboxID.Valid, "mailbox_id が記録されるべき")
	assert.Equal(t, mbID, uuid.UUID(act.MailboxID.Bytes))

	// 再取込みは dedup (message_id UNIQUE) で二重にならない。
	ingestMessages(ctx, q, msgs, "email_received", "system", pgtype.UUID{Bytes: mbID, Valid: true})
	// GetActivityByMessageID は :one なので重複してれば別途エラーになる。
	_, err = q.GetActivityByMessageID(ctx, pgtype.Text{String: msgID, Valid: true})
	require.NoError(t, err)
}

// CRM 管理外のアドレスからのメールは skip される。
func TestIngestMessages_SkipsUnknownCustomer(t *testing.T) {
	_, q := testutil.SetupTestDB(t)
	ctx := context.Background()
	msgID := "<" + uuid.NewString() + "@example.com>"
	ingestMessages(ctx, q, []ParsedMessage{{
		MessageID: msgID, From: "nobody-" + uuid.NewString() + "@nowhere.test", To: []string{"x@y.z"},
	}}, "email_received", "system", pgtype.UUID{Valid: false})

	_, err := q.GetActivityByMessageID(ctx, pgtype.Text{String: msgID, Valid: true})
	assert.Error(t, err, "未知顧客のメールは取込まれない")
}

// Phase 26: 管理対象メールボックス経由なら、未知の差出人でも MailboxMessage に
// 全件保存される (Activity は作られない)。既知顧客なら customer_id も付く。
func TestIngestMessages_StoresAllMailboxMessages(t *testing.T) {
	_, q := testutil.SetupTestDB(t)
	ctx := context.Background()
	cid := testutil.TestCompanyID(t, q)
	owner := testutil.TestUser(t, q, "ingest-mm-"+uuid.NewString(), cid)
	bk := testutil.TestBook(t, q, owner.ID)

	custMail := "known-" + uuid.NewString()[:8] + "@example.com"
	cust, err := q.CreateCustomer(ctx, db.CreateCustomerParams{
		ID: uuid.New(), BookID: bk.ID, Name: "既知顧客", Phone: "03-1111-1111", Mail: custMail,
	})
	require.NoError(t, err)

	mbID := uuid.New()
	_, err = q.CreateMailbox(ctx, db.CreateMailboxParams{
		ID: mbID, CompanyID: cid, Address: "mm-" + mbID.String() + "@0utl1er.tech",
		SmtpUsername: "mm@0utl1er.tech", PasswordEnc: []byte("x"), Active: true,
	})
	require.NoError(t, err)

	unknownID := "<" + uuid.NewString() + "@example.com>"
	knownID := "<" + uuid.NewString() + "@example.com>"
	msgs := []ParsedMessage{
		{
			MessageID: unknownID, Date: time.Now(), Subject: "新規のお問い合わせ",
			From: "prospect-" + uuid.NewString()[:8] + "@nowhere.test", To: []string{"mm@0utl1er.tech"},
			Body:            "はじめまして。サービスについて教えてください。",
			AttachmentNames: []string{"会社概要.pdf"},
		},
		{
			MessageID: knownID, Date: time.Now(), Subject: "既知顧客からの返信",
			From: custMail, To: []string{"mm@0utl1er.tech"},
			Body: "先日の件、承知しました。",
		},
	}
	ingestMessages(ctx, q, msgs, "email_received", "system", pgtype.UUID{Bytes: mbID, Valid: true})

	rows, err := q.ListMailboxMessages(ctx, db.ListMailboxMessagesParams{MailboxID: mbID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows, 2, "既知/未知どちらのメールも MailboxMessage に保存されるべき")

	byMsgID := map[string]db.ListMailboxMessagesRow{}
	for _, r := range rows {
		byMsgID[r.MessageID] = r
	}
	un := byMsgID[unknownID]
	assert.False(t, un.CustomerID.Valid, "未知差出人は customer_id なし")
	assert.Equal(t, "INBOX", un.Folder)
	assert.Contains(t, un.AttachmentNames, "会社概要.pdf")
	kn := byMsgID[knownID]
	require.True(t, kn.CustomerID.Valid, "既知顧客は customer_id 付き")
	assert.Equal(t, cust.ID, uuid.UUID(kn.CustomerID.Bytes))

	// 本文は Get で取れる。
	full, err := q.GetMailboxMessage(ctx, un.ID)
	require.NoError(t, err)
	assert.Contains(t, full.BodyText, "サービスについて")

	// 未知差出人の Activity は作られない。
	_, err = q.GetActivityByMessageID(ctx, pgtype.Text{String: unknownID, Valid: true})
	assert.Error(t, err)

	// 再取込みしても重複しない (mailbox_id+message_id UNIQUE)。
	ingestMessages(ctx, q, msgs, "email_received", "system", pgtype.UUID{Bytes: mbID, Valid: true})
	rows, err = q.ListMailboxMessages(ctx, db.ListMailboxMessagesParams{MailboxID: mbID, Limit: 10})
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}
