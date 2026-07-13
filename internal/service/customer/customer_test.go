package customer_test

import (
	"testing"

	"connectrpc.com/connect"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/search"
	"github.com/0utl1er-tech/phox-customer/internal/service/customer"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupCustomerTest(t *testing.T) (*customer.CustomerService, *db.Queries, db.User, db.Book) {
	t.Helper()
	_, queries := testutil.SetupTestDB(t)
	companyID := testutil.TestCompanyID(t, queries)
	user := testutil.TestUser(t, queries, "test-customer-user", companyID)
	book := testutil.TestBook(t, queries, user.ID)
	// ES なし (degraded mode) でも全 RPC が成功すること自体がテスト対象
	svc := customer.NewCustomerService(queries, search.NewIndexer(nil))
	return svc, queries, user, book
}

func TestCreateCustomer_Success(t *testing.T) {
	svc, _, user, book := setupCustomerTest(t)
	ctx := testutil.AuthContext(t, user.ID, "customer@test.com")

	resp, err := svc.CreateCustomer(ctx, connect.NewRequest(&customerv1.CreateCustomerRequest{
		BookId: book.ID.String(),
		Name:   "山田太郎",
		Phone:  "090-1234-5678",
		Memo:   "テストメモ",
	}))

	require.NoError(t, err)
	assert.Equal(t, "山田太郎", resp.Msg.Customer.Name)
	assert.Equal(t, "090-1234-5678", resp.Msg.Customer.Phone)
	assert.Equal(t, book.ID.String(), resp.Msg.Customer.BookId)
}

// 以前は uuid.MustParse がリクエスト入力で panic していた。
// 不正な UUID は CodeInvalidArgument で返ること。
func TestCreateCustomer_InvalidBookID(t *testing.T) {
	svc, _, user, _ := setupCustomerTest(t)
	ctx := testutil.AuthContext(t, user.ID, "customer@test.com")

	_, err := svc.CreateCustomer(ctx, connect.NewRequest(&customerv1.CreateCustomerRequest{
		BookId: "not-a-uuid",
		Name:   "x",
	}))

	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestCreateCustomer_NoPermit(t *testing.T) {
	svc, queries, _, book := setupCustomerTest(t)
	companyID := testutil.TestCompanyID(t, queries)
	outsider := testutil.TestUser(t, queries, "test-customer-outsider", companyID)
	ctx := testutil.AuthContext(t, outsider.ID, "outsider@test.com")

	_, err := svc.CreateCustomer(ctx, connect.NewRequest(&customerv1.CreateCustomerRequest{
		BookId: book.ID.String(),
		Name:   "侵入者",
	}))

	require.Error(t, err)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

func TestGetCustomer_Success(t *testing.T) {
	svc, queries, user, book := setupCustomerTest(t)
	ctx := testutil.AuthContext(t, user.ID, "customer@test.com")
	created := testutil.TestCustomer(t, queries, book.ID)

	resp, err := svc.GetCustomer(ctx, connect.NewRequest(&customerv1.GetCustomerRequest{
		Id: created.ID.String(),
	}))

	require.NoError(t, err)
	assert.Equal(t, created.ID.String(), resp.Msg.Customer.Id)
	assert.Equal(t, created.Name, resp.Msg.Customer.Name)
}

// GetCustomer は配下の contacts も返す (以前は常に空だった)。
func TestGetCustomer_IncludesContacts(t *testing.T) {
	svc, queries, user, book := setupCustomerTest(t)
	ctx := testutil.AuthContext(t, user.ID, "customer@test.com")
	created := testutil.TestCustomer(t, queries, book.ID)
	_, err := queries.CreateContact(ctx, db.CreateContactParams{
		ID: uuid.New(), CustomerID: created.ID, Name: "担当A", Mail: "a@example.com",
	})
	require.NoError(t, err)
	_, err = queries.CreateContact(ctx, db.CreateContactParams{
		ID: uuid.New(), CustomerID: created.ID, Name: "担当B", Mail: "b@example.com",
	})
	require.NoError(t, err)

	resp, err := svc.GetCustomer(ctx, connect.NewRequest(&customerv1.GetCustomerRequest{
		Id: created.ID.String(),
	}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.Customer.Contacts, 2, "contacts が同梱されるべき")
	mails := []string{resp.Msg.Customer.Contacts[0].Mail, resp.Msg.Customer.Contacts[1].Mail}
	assert.ElementsMatch(t, []string{"a@example.com", "b@example.com"}, mails)
}

func TestGetCustomer_InvalidID(t *testing.T) {
	svc, _, user, _ := setupCustomerTest(t)
	ctx := testutil.AuthContext(t, user.ID, "customer@test.com")

	_, err := svc.GetCustomer(ctx, connect.NewRequest(&customerv1.GetCustomerRequest{
		Id: "broken",
	}))

	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// 空文字列のフィールドは「未指定」扱いで既存値を保持する
// (util.OptionalText — 2026-04-21 の空文字列上書きバグの回帰テスト)。
func TestUpdateCustomer_EmptyStringKeepsExisting(t *testing.T) {
	svc, queries, user, book := setupCustomerTest(t)
	ctx := testutil.AuthContext(t, user.ID, "customer@test.com")
	created := testutil.TestCustomer(t, queries, book.ID)

	newName := "更新後の名前"
	empty := ""
	resp, err := svc.UpdateCustomer(ctx, connect.NewRequest(&customerv1.UpdateCustomerRequest{
		Id:    created.ID.String(),
		Name:  &newName,
		Phone: &empty, // 空文字列 → 既存の電話番号を保持
	}))

	require.NoError(t, err)
	assert.Equal(t, newName, resp.Msg.UpdatedCustomer.Name)
	assert.Equal(t, created.Phone, resp.Msg.UpdatedCustomer.Phone)
}

// viewer は更新不可 (owner/editor のみ)。
func TestUpdateCustomer_ViewerDenied(t *testing.T) {
	svc, queries, _, book := setupCustomerTest(t)
	companyID := testutil.TestCompanyID(t, queries)
	viewer := testutil.TestUser(t, queries, "test-customer-viewer", companyID)
	_, err := queries.CreatePermit(t.Context(), db.CreatePermitParams{
		ID:     uuid.New(),
		BookID: book.ID,
		UserID: viewer.ID,
		Role:   db.RoleViewer,
	})
	require.NoError(t, err)
	created := testutil.TestCustomer(t, queries, book.ID)
	ctx := testutil.AuthContext(t, viewer.ID, "viewer@test.com")

	name := "viewer による更新"
	_, err = svc.UpdateCustomer(ctx, connect.NewRequest(&customerv1.UpdateCustomerRequest{
		Id:   created.ID.String(),
		Name: &name,
	}))

	require.Error(t, err)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

func TestDeleteCustomer_Success(t *testing.T) {
	svc, queries, user, book := setupCustomerTest(t)
	ctx := testutil.AuthContext(t, user.ID, "customer@test.com")
	created := testutil.TestCustomer(t, queries, book.ID)

	resp, err := svc.DeleteCustomer(ctx, connect.NewRequest(&customerv1.DeleteCustomerRequest{
		CustomerId: created.ID.String(),
	}))

	require.NoError(t, err)
	assert.Equal(t, created.ID.String(), resp.Msg.CustomerId)

	_, err = svc.GetCustomer(ctx, connect.NewRequest(&customerv1.GetCustomerRequest{
		Id: created.ID.String(),
	}))
	require.Error(t, err)
}

func TestListCustomer_Success(t *testing.T) {
	svc, queries, user, book := setupCustomerTest(t)
	ctx := testutil.AuthContext(t, user.ID, "customer@test.com")
	testutil.TestCustomer(t, queries, book.ID)
	testutil.TestCustomer(t, queries, book.ID)

	resp, err := svc.ListCustomer(ctx, connect.NewRequest(&customerv1.ListCustomerRequest{
		BookId: book.ID.String(),
		Limit:  10,
	}))

	require.NoError(t, err)
	assert.Len(t, resp.Msg.Customers, 2)
}
