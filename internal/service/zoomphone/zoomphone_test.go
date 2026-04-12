package zoomphone_test

import (
	"testing"

	"connectrpc.com/connect"
	zoomphonev1 "github.com/0utl1er-tech/phox-customer/gen/pb/zoomphone/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/zoomphone"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMyZoomPhoneStatus_NoClient(t *testing.T) {
	_, queries := testutil.SetupTestDB(t)
	companyID := testutil.TestCompanyID(t, queries)
	user := testutil.TestUser(t, queries, "test-zp-user", companyID)

	svc := zoomphone.NewZoomPhoneService(queries, nil) // zoomClient=nil
	ctx := testutil.AuthContext(t, user.ID, "test@example.com")

	resp, err := svc.GetMyZoomPhoneStatus(ctx, connect.NewRequest(&zoomphonev1.GetMyZoomPhoneStatusRequest{}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.Connected) // Zoom 未設定なので false
}

func TestMakeCall_NoClient(t *testing.T) {
	_, queries := testutil.SetupTestDB(t)
	companyID := testutil.TestCompanyID(t, queries)
	user := testutil.TestUser(t, queries, "test-zp-user2", companyID)

	svc := zoomphone.NewZoomPhoneService(queries, nil)
	ctx := testutil.AuthContext(t, user.ID, "test@example.com")

	_, err := svc.MakeCall(ctx, connect.NewRequest(&zoomphonev1.MakeCallRequest{
		PhoneNumber: "03-1234-5678",
	}))
	require.Error(t, err)
	var connErr *connect.Error
	if assert.ErrorAs(t, err, &connErr) {
		assert.Equal(t, connect.CodeUnavailable, connErr.Code())
	}
}

func TestMakeCall_NoEmail(t *testing.T) {
	_, queries := testutil.SetupTestDB(t)
	companyID := testutil.TestCompanyID(t, queries)
	user := testutil.TestUser(t, queries, "test-zp-user3", companyID)

	// Zoom client は渡すが、email claim が無い context
	svc := zoomphone.NewZoomPhoneService(queries, nil)
	ctx := testutil.AuthContext(t, user.ID, "") // email 空

	_, err := svc.MakeCall(ctx, connect.NewRequest(&zoomphonev1.MakeCallRequest{
		PhoneNumber: "03-1234-5678",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "email")
}
