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
