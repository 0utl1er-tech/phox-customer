package icalfeed_test

import (
	"testing"

	"connectrpc.com/connect"
	icalfeedv1 "github.com/0utl1er-tech/phox-customer/gen/pb/icalfeed/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/icalfeed"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupICalTest(t *testing.T) (*icalfeed.ICalFeedService, string) {
	t.Helper()
	_, queries := testutil.SetupTestDB(t)
	companyID := testutil.TestCompanyID(t, queries)
	user := testutil.TestUser(t, queries, "test-ical-user", companyID)
	svc := icalfeed.NewICalFeedService(queries, "http://localhost:8082")
	return svc, user.ID
}

func TestGetICalFeed_NotGenerated(t *testing.T) {
	svc, userID := setupICalTest(t)
	ctx := testutil.AuthContext(t, userID, "ical@test.com")

	resp, err := svc.GetICalFeed(ctx, connect.NewRequest(&icalfeedv1.GetICalFeedRequest{}))
	require.NoError(t, err)
	assert.Nil(t, resp.Msg.Feed) // 未生成
}

func TestGenerateICalFeed_CreatesURL(t *testing.T) {
	svc, userID := setupICalTest(t)
	ctx := testutil.AuthContext(t, userID, "ical@test.com")

	resp, err := svc.GenerateICalFeed(ctx, connect.NewRequest(&icalfeedv1.GenerateICalFeedRequest{}))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Msg.Feed.Url)
	assert.Contains(t, resp.Msg.Feed.Url, "webcal://")
	assert.Contains(t, resp.Msg.Feed.Url, ".ics")
}

func TestGenerateICalFeed_Idempotent(t *testing.T) {
	svc, userID := setupICalTest(t)
	ctx := testutil.AuthContext(t, userID, "ical@test.com")

	resp1, _ := svc.GenerateICalFeed(ctx, connect.NewRequest(&icalfeedv1.GenerateICalFeedRequest{}))
	resp2, _ := svc.GenerateICalFeed(ctx, connect.NewRequest(&icalfeedv1.GenerateICalFeedRequest{}))
	assert.Equal(t, resp1.Msg.Feed.Url, resp2.Msg.Feed.Url) // 同じ URL
}

func TestRegenerateICalFeed_ChangesURL(t *testing.T) {
	svc, userID := setupICalTest(t)
	ctx := testutil.AuthContext(t, userID, "ical@test.com")

	gen, _ := svc.GenerateICalFeed(ctx, connect.NewRequest(&icalfeedv1.GenerateICalFeedRequest{}))
	regen, err := svc.RegenerateICalFeed(ctx, connect.NewRequest(&icalfeedv1.RegenerateICalFeedRequest{}))
	require.NoError(t, err)
	assert.NotEqual(t, gen.Msg.Feed.Url, regen.Msg.Feed.Url) // 別の URL
}

func TestRevokeICalFeed_ThenGetReturnsNil(t *testing.T) {
	svc, userID := setupICalTest(t)
	ctx := testutil.AuthContext(t, userID, "ical@test.com")

	svc.GenerateICalFeed(ctx, connect.NewRequest(&icalfeedv1.GenerateICalFeedRequest{}))

	_, err := svc.RevokeICalFeed(ctx, connect.NewRequest(&icalfeedv1.RevokeICalFeedRequest{}))
	require.NoError(t, err)

	getResp, _ := svc.GetICalFeed(ctx, connect.NewRequest(&icalfeedv1.GetICalFeedRequest{}))
	assert.Nil(t, getResp.Msg.Feed)
}
