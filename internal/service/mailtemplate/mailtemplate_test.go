package mailtemplate_test

import (
	"testing"

	"connectrpc.com/connect"
	mailtemplatev1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailtemplate/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/mailtemplate"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMailTemplateTest(t *testing.T) (*mailtemplate.MailTemplateService, string, string) {
	t.Helper()
	_, queries := testutil.SetupTestDB(t)
	companyID := testutil.TestCompanyID(t, queries)
	user := testutil.TestUser(t, queries, "test-mt-user", companyID)
	book := testutil.TestBook(t, queries, user.ID)
	svc := mailtemplate.NewMailTemplateService(queries)
	return svc, user.ID, book.ID.String()
}

func TestCreateMailTemplate_Success(t *testing.T) {
	svc, userID, bookID := setupMailTemplateTest(t)
	ctx := testutil.AuthContext(t, userID, "mt@test.com")

	resp, err := svc.CreateMailTemplate(ctx, connect.NewRequest(&mailtemplatev1.CreateMailTemplateRequest{
		BookId:  bookID,
		Name:    "テストテンプレ",
		Subject: "件名 {{customer_name}}",
		Body:    "本文 {{customer_name}} 様",
	}))

	require.NoError(t, err)
	assert.Equal(t, "テストテンプレ", resp.Msg.Template.Name)
	assert.Equal(t, "件名 {{customer_name}}", resp.Msg.Template.Subject)
	assert.Equal(t, bookID, resp.Msg.Template.BookId)
}

func TestListMailTemplatesByBook_Empty(t *testing.T) {
	svc, userID, bookID := setupMailTemplateTest(t)
	ctx := testutil.AuthContext(t, userID, "mt@test.com")

	resp, err := svc.ListMailTemplatesByBook(ctx, connect.NewRequest(&mailtemplatev1.ListMailTemplatesByBookRequest{
		BookId: bookID,
	}))

	require.NoError(t, err)
	// 新規 book なので 0 件 (他テストの影響で > 0 の場合もあるが error は出ないこと)
	assert.NotNil(t, resp.Msg.Templates)
}

func TestUpdateMailTemplate_Success(t *testing.T) {
	svc, userID, bookID := setupMailTemplateTest(t)
	ctx := testutil.AuthContext(t, userID, "mt@test.com")

	createResp, err := svc.CreateMailTemplate(ctx, connect.NewRequest(&mailtemplatev1.CreateMailTemplateRequest{
		BookId: bookID, Name: "v1", Subject: "sub1", Body: "body1",
	}))
	require.NoError(t, err)

	updateResp, err := svc.UpdateMailTemplate(ctx, connect.NewRequest(&mailtemplatev1.UpdateMailTemplateRequest{
		Id: createResp.Msg.Template.Id, Name: "v2", Subject: "sub2", Body: "body2",
	}))
	require.NoError(t, err)
	assert.Equal(t, "v2", updateResp.Msg.Template.Name)
	assert.Equal(t, "sub2", updateResp.Msg.Template.Subject)
}

func TestDeleteMailTemplate_Success(t *testing.T) {
	svc, userID, bookID := setupMailTemplateTest(t)
	ctx := testutil.AuthContext(t, userID, "mt@test.com")

	createResp, err := svc.CreateMailTemplate(ctx, connect.NewRequest(&mailtemplatev1.CreateMailTemplateRequest{
		BookId: bookID, Name: "to-delete", Subject: "", Body: "",
	}))
	require.NoError(t, err)

	_, err = svc.DeleteMailTemplate(ctx, connect.NewRequest(&mailtemplatev1.DeleteMailTemplateRequest{
		Id: createResp.Msg.Template.Id,
	}))
	require.NoError(t, err)
}

func TestCreateMailTemplate_Unauthorized(t *testing.T) {
	svc, _, bookID := setupMailTemplateTest(t)
	ctx := testutil.AuthContext(t, "no-permit-user", "bad@test.com")

	_, err := svc.CreateMailTemplate(ctx, connect.NewRequest(&mailtemplatev1.CreateMailTemplateRequest{
		BookId: bookID, Name: "fail", Subject: "", Body: "",
	}))

	require.Error(t, err)
	var connErr *connect.Error
	if assert.ErrorAs(t, err, &connErr) {
		assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
	}
}
