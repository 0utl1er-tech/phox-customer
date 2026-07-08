package mailbox_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	"github.com/0utl1er-tech/phox-customer/internal/service/mailbox"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// テスト用 32byte 鍵 (base64)。
const testKeyB64 = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

// newFixture は fresh な company + 2 ユーザー (owner 候補 / other) を作る。
// ユーザー ID は uuid で一意化し、共有テスト DB に前回実行のデータが残っていても
// 衝突しないようにする (件数アサートも避ける)。
func newFixture(t *testing.T) (*mailbox.MailboxService, *db.Queries, db.User, db.User) {
	t.Helper()
	_, q := testutil.SetupTestDB(t)
	cid := testutil.TestCompanyID(t, q)
	owner := testutil.TestUser(t, q, "mb-owner-"+uuid.NewString(), cid)
	other := testutil.TestUser(t, q, "mb-other-"+uuid.NewString(), cid)
	cipher, err := crypto.NewCipherFromBase64(testKeyB64)
	require.NoError(t, err)
	return mailbox.NewMailboxService(q, cipher, nil), q, owner, other
}

func uniqAddr(prefix string) string { return prefix + "-" + uuid.NewString() + "@0utl1er.tech" }

// findByID は一覧から id のメールボックスを探す (無ければ nil)。
func findByID(list []*mailboxv1.Mailbox, id string) *mailboxv1.Mailbox {
	for _, m := range list {
		if m.Id == id {
			return m
		}
	}
	return nil
}

func TestCreateAndListMailbox(t *testing.T) {
	svc, q, owner, _ := newFixture(t)
	ctx := testutil.AuthContext(t, owner.ID, "owner@test.com")

	resp, err := svc.CreateMailbox(ctx, connect.NewRequest(&mailboxv1.CreateMailboxRequest{
		Address:     uniqAddr("sales"),
		Password:    "s3cr3t-pw",
		DisplayName: "Sales",
	}))
	require.NoError(t, err)
	require.NotEmpty(t, resp.Msg.Mailbox.Id)
	assert.Equal(t, permitv1.Role_ROLE_OWNER, resp.Msg.Mailbox.Role, "作成者は owner")

	// パスワードは DB に平文で入っていない (暗号化されている)。
	mbID, err := uuid.Parse(resp.Msg.Mailbox.Id)
	require.NoError(t, err)
	row, err := q.GetMailbox(ctx, mbID)
	require.NoError(t, err)
	assert.NotContains(t, string(row.PasswordEnc), "s3cr3t-pw", "平文パスワードが保存されている")
	assert.NotEmpty(t, row.PasswordEnc)

	// ListMailboxes に (owner ロールで) 現れる。
	list, err := svc.ListMailboxes(ctx, connect.NewRequest(&mailboxv1.ListMailboxesRequest{}))
	require.NoError(t, err)
	m := findByID(list.Msg.Mailboxes, resp.Msg.Mailbox.Id)
	require.NotNil(t, m, "作成したメールボックスが一覧に出るべき")
	assert.Equal(t, "Sales", m.DisplayName)
	assert.Equal(t, permitv1.Role_ROLE_OWNER, m.Role)
}

func TestListMailboxes_OnlyPermitted(t *testing.T) {
	svc, _, owner, other := newFixture(t)
	octx := testutil.AuthContext(t, owner.ID, "owner@test.com")
	created, err := svc.CreateMailbox(octx, connect.NewRequest(&mailboxv1.CreateMailboxRequest{
		Address: uniqAddr("sup"), Password: "pw",
	}))
	require.NoError(t, err)

	// permit の無い other には見えない。
	xctx := testutil.AuthContext(t, other.ID, "other@test.com")
	list, err := svc.ListMailboxes(xctx, connect.NewRequest(&mailboxv1.ListMailboxesRequest{}))
	require.NoError(t, err)
	assert.Nil(t, findByID(list.Msg.Mailboxes, created.Msg.Mailbox.Id), "permit の無いユーザーには見えない")
}

func TestAddMailboxUser_RBAC(t *testing.T) {
	svc, _, owner, other := newFixture(t)
	octx := testutil.AuthContext(t, owner.ID, "owner@test.com")
	created, err := svc.CreateMailbox(octx, connect.NewRequest(&mailboxv1.CreateMailboxRequest{
		Address: uniqAddr("team"), Password: "pw",
	}))
	require.NoError(t, err)
	mbID := created.Msg.Mailbox.Id

	// owner が editor を付与。
	_, err = svc.AddMailboxUser(octx, connect.NewRequest(&mailboxv1.AddMailboxUserRequest{
		MailboxId: mbID, UserId: other.ID, Role: permitv1.Role_ROLE_EDITOR,
	}))
	require.NoError(t, err)

	// other は editor になったので ListMailboxes に editor ロールで出る。
	xctx := testutil.AuthContext(t, other.ID, "other@test.com")
	list, err := svc.ListMailboxes(xctx, connect.NewRequest(&mailboxv1.ListMailboxesRequest{}))
	require.NoError(t, err)
	m := findByID(list.Msg.Mailboxes, mbID)
	require.NotNil(t, m)
	assert.Equal(t, permitv1.Role_ROLE_EDITOR, m.Role)

	// editor の other が誰かを追加しようとすると owner 権限が無く拒否。
	_, err = svc.AddMailboxUser(xctx, connect.NewRequest(&mailboxv1.AddMailboxUserRequest{
		MailboxId: mbID, UserId: owner.ID, Role: permitv1.Role_ROLE_VIEWER,
	}))
	assertConnectCode(t, err, connect.CodePermissionDenied)

	// owner 付与は不可。
	_, err = svc.AddMailboxUser(octx, connect.NewRequest(&mailboxv1.AddMailboxUserRequest{
		MailboxId: mbID, UserId: other.ID, Role: permitv1.Role_ROLE_OWNER,
	}))
	assertConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestCreateMailbox_DuplicateAddress(t *testing.T) {
	svc, _, owner, _ := newFixture(t)
	ctx := testutil.AuthContext(t, owner.ID, "owner@test.com")
	addr := uniqAddr("dup")
	_, err := svc.CreateMailbox(ctx, connect.NewRequest(&mailboxv1.CreateMailboxRequest{Address: addr, Password: "pw"}))
	require.NoError(t, err)
	_, err = svc.CreateMailbox(ctx, connect.NewRequest(&mailboxv1.CreateMailboxRequest{Address: addr, Password: "pw"}))
	assertConnectCode(t, err, connect.CodeAlreadyExists)
}

func TestDeleteMailbox_OwnerOnly(t *testing.T) {
	svc, _, owner, other := newFixture(t)
	octx := testutil.AuthContext(t, owner.ID, "owner@test.com")
	created, err := svc.CreateMailbox(octx, connect.NewRequest(&mailboxv1.CreateMailboxRequest{
		Address: uniqAddr("del"), Password: "pw",
	}))
	require.NoError(t, err)
	// permit 無し → 権限拒否。
	xctx := testutil.AuthContext(t, other.ID, "other@test.com")
	_, err = svc.DeleteMailbox(xctx, connect.NewRequest(&mailboxv1.DeleteMailboxRequest{Id: created.Msg.Mailbox.Id}))
	assertConnectCode(t, err, connect.CodePermissionDenied)
	// owner → 成功。
	_, err = svc.DeleteMailbox(octx, connect.NewRequest(&mailboxv1.DeleteMailboxRequest{Id: created.Msg.Mailbox.Id}))
	require.NoError(t, err)
}

func TestUpdateMailbox_PasswordRotates(t *testing.T) {
	svc, q, owner, _ := newFixture(t)
	ctx := testutil.AuthContext(t, owner.ID, "owner@test.com")
	created, err := svc.CreateMailbox(ctx, connect.NewRequest(&mailboxv1.CreateMailboxRequest{
		Address: uniqAddr("upd"), Password: "old-pw",
	}))
	require.NoError(t, err)
	mbID, _ := uuid.Parse(created.Msg.Mailbox.Id)
	before, _ := q.GetMailbox(ctx, mbID)

	newPw := "new-pw"
	_, err = svc.UpdateMailbox(ctx, connect.NewRequest(&mailboxv1.UpdateMailboxRequest{
		Id: created.Msg.Mailbox.Id, Password: &newPw,
	}))
	require.NoError(t, err)
	after, _ := q.GetMailbox(ctx, mbID)
	assert.NotEqual(t, before.PasswordEnc, after.PasswordEnc, "パスワード更新で暗号文が変わる")
	assert.False(t, strings.Contains(string(after.PasswordEnc), newPw), "平文で保存しない")
}

// ─── helpers ─────────────────────────────────────────────────

func assertConnectCode(t *testing.T, err error, want connect.Code) {
	t.Helper()
	require.Error(t, err)
	var cerr *connect.Error
	require.ErrorAs(t, err, &cerr)
	assert.Equal(t, want, cerr.Code(), "connect code")
}
