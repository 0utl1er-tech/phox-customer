package mailbox

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	"github.com/0utl1er-tech/phox-customer/internal/mailu"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const adoptKeyB64 = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

// fakeProvisioner は mailu 操作を記録するテスト用フェイク。
type fakeProvisioner struct {
	createErr error
	created   []string
	setPw     []string
	deleted   []string
}

func (f *fakeProvisioner) CreateUser(_ context.Context, email, _, _ string) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, email)
	return nil
}
func (f *fakeProvisioner) SetPassword(_ context.Context, email, _ string) error {
	f.setPw = append(f.setPw, email)
	return nil
}
func (f *fakeProvisioner) DeleteUser(_ context.Context, email string) error {
	f.deleted = append(f.deleted, email)
	return nil
}

func newSvcWithProvisioner(t *testing.T, p mailuProvisioner) (*MailboxService, string) {
	t.Helper()
	_, q := testutil.SetupTestDB(t)
	cid := testutil.TestCompanyID(t, q)
	owner := testutil.TestUser(t, q, "adopt-owner-"+uuid.NewString(), cid)
	cipher, err := crypto.NewCipherFromBase64(adoptKeyB64)
	require.NoError(t, err)
	svc := NewMailboxService(q, cipher, nil)
	svc.provisioner = p // 白箱テスト: フェイクを注入
	return svc, owner.ID
}

// mailu に既に存在するアドレス (409) は弾かず取り込む。
func TestCreateMailbox_AdoptsExistingMailuAccount(t *testing.T) {
	fp := &fakeProvisioner{createErr: mailu.ErrConflict}
	svc, ownerID := newSvcWithProvisioner(t, fp)
	ctx := testutil.AuthContext(t, ownerID, "owner@test.com")
	addr := "info-" + uuid.NewString() + "@0utl1er.tech"

	resp, err := svc.CreateMailbox(ctx, connect.NewRequest(&mailboxv1.CreateMailboxRequest{
		Address: addr, Password: "existing-pw", DisplayName: "営業窓口",
	}))
	require.NoError(t, err, "既存アカウントは取り込むべき (エラーにしない)")
	require.NotEmpty(t, resp.Msg.Mailbox.Id)

	assert.Equal(t, []string{addr}, fp.setPw, "取り込み時は SetPassword で資格情報を揃える")
	assert.Empty(t, fp.created, "既存なので CreateUser は成功していない")
	assert.Empty(t, fp.deleted, "取り込んだ既存アカウントは削除しない")

	// DB に登録され、パスワードは暗号化されている。
	mbID, _ := uuid.Parse(resp.Msg.Mailbox.Id)
	row, err := svc.queries.GetMailbox(ctx, mbID)
	require.NoError(t, err)
	assert.NotContains(t, string(row.PasswordEnc), "existing-pw")
	assert.NotEmpty(t, row.PasswordEnc)
}

// 新規作成 (409 でない) 時は CreateUser のみ、SetPassword は呼ばない。
func TestCreateMailbox_CreatesNewMailuAccount(t *testing.T) {
	fp := &fakeProvisioner{}
	svc, ownerID := newSvcWithProvisioner(t, fp)
	ctx := testutil.AuthContext(t, ownerID, "owner@test.com")
	addr := "new-" + uuid.NewString() + "@0utl1er.tech"

	_, err := svc.CreateMailbox(ctx, connect.NewRequest(&mailboxv1.CreateMailboxRequest{
		Address: addr, Password: "", DisplayName: "新規",
	}))
	require.NoError(t, err)
	assert.Equal(t, []string{addr}, fp.created, "新規は CreateUser で作る")
	assert.Empty(t, fp.setPw, "新規作成では SetPassword しない")
}
