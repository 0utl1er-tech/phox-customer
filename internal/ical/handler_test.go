package ical_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/ical"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_NotFound(t *testing.T) {
	_, queries := testutil.SetupTestDB(t)
	handler := ical.NewHandler(queries, "http://localhost:3000")

	req := httptest.NewRequest("GET", "/ical/nonexistent-token.ics", nil)
	req.SetPathValue("filename", "nonexistent-token.ics")
	rec := httptest.NewRecorder()
	handler.Serve(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandler_EmptyToken(t *testing.T) {
	_, queries := testutil.SetupTestDB(t)
	handler := ical.NewHandler(queries, "http://localhost:3000")

	req := httptest.NewRequest("GET", "/ical/.ics", nil)
	req.SetPathValue("filename", ".ics")
	rec := httptest.NewRecorder()
	handler.Serve(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandler_ValidFeed(t *testing.T) {
	_, queries := testutil.SetupTestDB(t)
	companyID := testutil.TestCompanyID(t, queries)
	user := testutil.TestUser(t, queries, "ical-handler-test", companyID)

	// Feed トークンを生成
	feed, err := queries.UpsertUserICalFeed(context.Background(), db.UpsertUserICalFeedParams{
		UserID: user.ID,
		Token:  "test-handler-token-12345",
	})
	require.NoError(t, err)

	handler := ical.NewHandler(queries, "http://localhost:3000")
	req := httptest.NewRequest("GET", "/ical/"+feed.Token+".ics", nil)
	req.SetPathValue("filename", feed.Token+".ics")
	rec := httptest.NewRecorder()
	handler.Serve(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/calendar")
	assert.Contains(t, rec.Body.String(), "BEGIN:VCALENDAR")
	assert.Contains(t, rec.Body.String(), "PRODID:-//Phox")
	assert.NotEmpty(t, rec.Header().Get("ETag"))
}

func TestHandler_ETag304(t *testing.T) {
	_, queries := testutil.SetupTestDB(t)
	companyID := testutil.TestCompanyID(t, queries)
	user := testutil.TestUser(t, queries, "ical-etag-test", companyID)

	queries.UpsertUserICalFeed(context.Background(), db.UpsertUserICalFeedParams{
		UserID: user.ID,
		Token:  "test-etag-token-67890",
	})

	handler := ical.NewHandler(queries, "http://localhost:3000")

	// 1st request — get ETag
	req1 := httptest.NewRequest("GET", "/ical/test-etag-token-67890.ics", nil)
	req1.SetPathValue("filename", "test-etag-token-67890.ics")
	rec1 := httptest.NewRecorder()
	handler.Serve(rec1, req1)
	require.Equal(t, 200, rec1.Code)
	etag := rec1.Header().Get("ETag")
	require.NotEmpty(t, etag)

	// 2nd request with If-None-Match — should get 304
	req2 := httptest.NewRequest("GET", "/ical/test-etag-token-67890.ics", nil)
	req2.SetPathValue("filename", "test-etag-token-67890.ics")
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	handler.Serve(rec2, req2)
	assert.Equal(t, http.StatusNotModified, rec2.Code)
}
