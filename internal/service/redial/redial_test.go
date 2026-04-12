package redial_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	redialv1 "github.com/0utl1er-tech/phox-customer/gen/pb/redial/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/gcal"
	"github.com/0utl1er-tech/phox-customer/internal/service/redial"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func setupRedialTest(t *testing.T) (*redial.RedialService, *redial.RedialService, db.Customer, string) {
	t.Helper()
	_, queries := testutil.SetupTestDB(t)
	companyID := testutil.TestCompanyID(t, queries)
	user := testutil.TestUser(t, queries, "test-redial-user", companyID)
	book := testutil.TestBook(t, queries, user.ID)
	customer := testutil.TestCustomer(t, queries, book.ID)

	mockGcal := gcal.NewMockClient()
	svcWithGcal := redial.NewRedialService(queries, mockGcal)
	svcNoGcal := redial.NewRedialService(queries, nil)

	return svcWithGcal, svcNoGcal, customer, user.ID
}

func futureTimestamps() (*timestamppb.Timestamp, *timestamppb.Timestamp) {
	start := time.Now().Add(24 * time.Hour)
	end := start.Add(30 * time.Minute)
	return timestamppb.New(start), timestamppb.New(end)
}

func TestCreateRedial_Success_WithMockGcal(t *testing.T) {
	svc, _, customer, userID := setupRedialTest(t)
	ctx := testutil.AuthContext(t, userID, "redial@test.com")

	startAt, endAt := futureTimestamps()
	resp, err := svc.CreateRedial(ctx, connect.NewRequest(&redialv1.CreateRedialRequest{
		CustomerId: customer.ID.String(),
		Phone:      "03-1111-2222",
		StartAt:    startAt,
		EndAt:      endAt,
		Note:       "テスト掛け直し",
	}))

	require.NoError(t, err)
	assert.NotEmpty(t, resp.Msg.Redial.Id)
	assert.Equal(t, "テスト掛け直し", resp.Msg.Redial.Note)
	// GCal mock が使われているが、UserGoogleToken が無いので not_connected
	assert.Equal(t, redialv1.SyncStatus_SYNC_STATUS_NOT_CONNECTED, resp.Msg.Redial.SyncStatus)
}

func TestCreateRedial_Success_NoGcal(t *testing.T) {
	_, svc, customer, userID := setupRedialTest(t)
	ctx := testutil.AuthContext(t, userID, "redial@test.com")

	startAt, endAt := futureTimestamps()
	resp, err := svc.CreateRedial(ctx, connect.NewRequest(&redialv1.CreateRedialRequest{
		CustomerId: customer.ID.String(),
		Phone:      "090-0000-0000",
		StartAt:    startAt,
		EndAt:      endAt,
		Note:       "GCal なし",
	}))

	require.NoError(t, err)
	assert.NotEmpty(t, resp.Msg.Redial.Id)
	assert.Equal(t, redialv1.SyncStatus_SYNC_STATUS_NOT_CONNECTED, resp.Msg.Redial.SyncStatus)
}

func TestCreateRedial_MissingStartAt(t *testing.T) {
	svc, _, customer, userID := setupRedialTest(t)
	ctx := testutil.AuthContext(t, userID, "redial@test.com")

	_, err := svc.CreateRedial(ctx, connect.NewRequest(&redialv1.CreateRedialRequest{
		CustomerId: customer.ID.String(),
		Phone:      "03-1234-5678",
		// StartAt/EndAt 未設定
	}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_at")
}

func TestCreateRedial_Unauthorized(t *testing.T) {
	svc, _, customer, _ := setupRedialTest(t)
	ctx := testutil.AuthContext(t, "no-access-user", "bad@test.com")

	startAt, endAt := futureTimestamps()
	_, err := svc.CreateRedial(ctx, connect.NewRequest(&redialv1.CreateRedialRequest{
		CustomerId: customer.ID.String(),
		Phone:      "03-1234-5678",
		StartAt:    startAt,
		EndAt:      endAt,
	}))

	require.Error(t, err)
	var connErr *connect.Error
	if assert.ErrorAs(t, err, &connErr) {
		assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
	}
}

func TestListRedialsByCustomer_Success(t *testing.T) {
	svc, _, customer, userID := setupRedialTest(t)
	ctx := testutil.AuthContext(t, userID, "redial@test.com")

	// 1 件作成
	startAt, endAt := futureTimestamps()
	_, err := svc.CreateRedial(ctx, connect.NewRequest(&redialv1.CreateRedialRequest{
		CustomerId: customer.ID.String(), Phone: "090-1111", StartAt: startAt, EndAt: endAt,
	}))
	require.NoError(t, err)

	resp, err := svc.ListRedialsByCustomer(ctx, connect.NewRequest(&redialv1.ListRedialsByCustomerRequest{
		CustomerId: customer.ID.String(),
	}))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(resp.Msg.Redials), 1)
}

func TestDeleteRedial_Success(t *testing.T) {
	svc, _, customer, userID := setupRedialTest(t)
	ctx := testutil.AuthContext(t, userID, "redial@test.com")

	startAt, endAt := futureTimestamps()
	createResp, err := svc.CreateRedial(ctx, connect.NewRequest(&redialv1.CreateRedialRequest{
		CustomerId: customer.ID.String(), Phone: "03-delete", StartAt: startAt, EndAt: endAt,
	}))
	require.NoError(t, err)

	_, err = svc.DeleteRedial(ctx, connect.NewRequest(&redialv1.DeleteRedialRequest{
		Id: createResp.Msg.Redial.Id,
	}))
	require.NoError(t, err)
}

func TestUpdateRedial_Success(t *testing.T) {
	svc, _, customer, userID := setupRedialTest(t)
	ctx := testutil.AuthContext(t, userID, "redial@test.com")

	startAt, endAt := futureTimestamps()
	createResp, err := svc.CreateRedial(ctx, connect.NewRequest(&redialv1.CreateRedialRequest{
		CustomerId: customer.ID.String(), Phone: "03-update", StartAt: startAt, EndAt: endAt, Note: "before",
	}))
	require.NoError(t, err)

	newStart, newEnd := futureTimestamps()
	updateResp, err := svc.UpdateRedial(ctx, connect.NewRequest(&redialv1.UpdateRedialRequest{
		Id: createResp.Msg.Redial.Id, Phone: "03-updated", StartAt: newStart, EndAt: newEnd, Note: "after",
	}))
	require.NoError(t, err)
	assert.Equal(t, "after", updateResp.Msg.Redial.Note)
	assert.Equal(t, "03-updated", updateResp.Msg.Redial.Phone)
}
