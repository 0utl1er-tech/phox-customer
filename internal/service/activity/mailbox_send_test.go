package activity_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	"github.com/0utl1er-tech/phox-customer/internal/mail"
	"github.com/0utl1er-tech/phox-customer/internal/service/activity"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// Phase 25: mailbox_id 指定送信の認可・前提条件チェック。
// 実 SMTP 送信のハッピーパスは staging E2E (MailHog) が担当し、ここでは
// 「送信前に落ちるべきものが送信前に落ちる」ことを検証する (SendAs は
// dial 前に権限/复号エラーで到達しない)。
func TestCreateActivityEmailSent_MailboxPath(t *testing.T) {
	_, q := testutil.SetupTestDB(t)
	ctx := context.Background()
	cid := testutil.TestCompanyID(t, q)
	sender := testutil.TestUser(t, q, "mbsend-user-"+uuid.NewString(), cid)
	bk := testutil.TestBook(t, q, sender.ID)
	cust := testutil.TestCustomer(t, q, bk.ID)

	cipher, err := crypto.NewCipherFromBase64("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	require.NoError(t, err)
	// 到達しないダミー SMTP (dial は権限チェック通過後にしか起きない)。
	mbSender, err := mail.NewMailboxSender("smtp.invalid", 1, "none")
	require.NoError(t, err)
	require.NotNil(t, mbSender)

	enc, err := cipher.EncryptString("mb-pw")
	require.NoError(t, err)
	mbID := uuid.New()
	_, err = q.CreateMailbox(ctx, db.CreateMailboxParams{
		ID: mbID, CompanyID: cid, Address: "mb-" + mbID.String() + "@0utl1er.tech",
		SmtpUsername: "mb@0utl1er.tech", PasswordEnc: enc, Active: true,
	})
	require.NoError(t, err)

	authCtx := testutil.AuthContext(t, sender.ID, "sender@test.com")
	baseReq := func() *activityv1.CreateActivityEmailSentRequest {
		return &activityv1.CreateActivityEmailSentRequest{
			CustomerId: cust.ID.String(),
			MailTo:     "someone@example.com",
			Subject:    "test",
			MailboxId:  proto.String(mbID.String()),
		}
	}

	t.Run("未設定 (WithMailboxSending なし) は Unavailable", func(t *testing.T) {
		svc := activity.NewActivityService(q, nil, nil)
		_, err := svc.CreateActivityEmailSent(authCtx, connect.NewRequest(baseReq()))
		assertCode(t, err, connect.CodeUnavailable)
	})

	svc := activity.NewActivityService(q, nil, nil).WithMailboxSending(mbSender, cipher)

	t.Run("MailboxPermit が無ければ PermissionDenied", func(t *testing.T) {
		_, err := svc.CreateActivityEmailSent(authCtx, connect.NewRequest(baseReq()))
		assertCode(t, err, connect.CodePermissionDenied)
	})

	t.Run("viewer では PermissionDenied (editor 必須)", func(t *testing.T) {
		_, err := q.CreateMailboxPermit(ctx, db.CreateMailboxPermitParams{
			ID: uuid.New(), MailboxID: mbID, UserID: sender.ID, Role: db.RoleViewer,
		})
		require.NoError(t, err)
		_, err = svc.CreateActivityEmailSent(authCtx, connect.NewRequest(baseReq()))
		assertCode(t, err, connect.CodePermissionDenied)
	})

	t.Run("editor + inactive mailbox は FailedPrecondition", func(t *testing.T) {
		_, err := q.UpdateMailboxPermitRole(ctx, db.UpdateMailboxPermitRoleParams{
			MailboxID: mbID, UserID: sender.ID, Role: db.RoleEditor,
		})
		require.NoError(t, err)
		_, err = q.UpdateMailbox(ctx, db.UpdateMailboxParams{ID: mbID, Active: pgtype.Bool{Bool: false, Valid: true}})
		require.NoError(t, err)
		_, err = svc.CreateActivityEmailSent(authCtx, connect.NewRequest(baseReq()))
		assertCode(t, err, connect.CodeFailedPrecondition)
	})

	t.Run("editor + active だが SMTP 不達なら Internal (認可は通過)", func(t *testing.T) {
		_, err := q.UpdateMailbox(ctx, db.UpdateMailboxParams{ID: mbID, Active: pgtype.Bool{Bool: true, Valid: true}})
		require.NoError(t, err)
		_, err = svc.CreateActivityEmailSent(authCtx, connect.NewRequest(baseReq()))
		assertCode(t, err, connect.CodeInternal)
	})
}

func assertCode(t *testing.T, err error, want connect.Code) {
	t.Helper()
	require.Error(t, err)
	var cerr *connect.Error
	require.ErrorAs(t, err, &cerr)
	assert.Equal(t, want, cerr.Code())
}
