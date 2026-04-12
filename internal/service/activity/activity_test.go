package activity_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/activity"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── fixture ────────────────────────────────────────────────────

type activityFixture struct {
	svc      *activity.ActivityService
	queries  *db.Queries
	customer db.Customer
	userID   string
	statusID string
}

func newFixture(t *testing.T) activityFixture {
	t.Helper()
	_, q := testutil.SetupTestDB(t)
	cid := testutil.TestCompanyID(t, q)
	u := testutil.TestUser(t, q, "act-"+t.Name(), cid)
	b := testutil.TestBook(t, q, u.ID)
	c := testutil.TestCustomer(t, q, b.ID)
	s, err := q.GetDefaultStatusByBookID(context.Background(), c.BookID)
	require.NoError(t, err)
	return activityFixture{
		svc: activity.NewActivityService(q, nil), queries: q,
		customer: c, userID: u.ID, statusID: s.ID.String(),
	}
}

// ─── CreateActivityCall ─────────────────────────────────────────

func TestCreateActivityCall(t *testing.T) {
	f := newFixture(t)

	tests := []struct {
		name      string
		ctx       context.Context
		req       *activityv1.CreateActivityCallRequest
		wantCode  connect.Code
		wantErr   string
		checkResp func(*testing.T, *activityv1.CreateActivityCallResponse)
	}{
		{
			name: "正常系/コール履歴が作成される",
			ctx:  testutil.AuthContext(t, f.userID, "ok@test.com"),
			req: &activityv1.CreateActivityCallRequest{
				CustomerId: f.customer.ID.String(), Phone: "03-1234-5678", StatusId: f.statusID,
			},
			checkResp: func(t *testing.T, r *activityv1.CreateActivityCallResponse) {
				assert.NotEmpty(t, r.Activity.Id)
				assert.Equal(t, activityv1.ActivityType_ACTIVITY_TYPE_CALL, r.Activity.Type)
				assert.Equal(t, "03-1234-5678", *r.Activity.Phone)
			},
		},
		{
			name:     "異常系/customer_id が不正",
			ctx:      testutil.AuthContext(t, f.userID, "ok@test.com"),
			req:      &activityv1.CreateActivityCallRequest{CustomerId: "bad", Phone: "03", StatusId: f.statusID},
			wantCode: connect.CodeInvalidArgument, wantErr: "invalid customer_id",
		},
		{
			name:     "異常系/status_id が不正",
			ctx:      testutil.AuthContext(t, f.userID, "ok@test.com"),
			req:      &activityv1.CreateActivityCallRequest{CustomerId: f.customer.ID.String(), Phone: "03", StatusId: "bad"},
			wantCode: connect.CodeInvalidArgument, wantErr: "invalid status_id",
		},
		{
			name:     "異常系/存在しない customer",
			ctx:      testutil.AuthContext(t, f.userID, "ok@test.com"),
			req:      &activityv1.CreateActivityCallRequest{CustomerId: "00000000-0000-0000-0000-000000000000", Phone: "03", StatusId: f.statusID},
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "異常系/権限なし",
			ctx:      testutil.AuthContext(t, "outsider", "bad@test.com"),
			req:      &activityv1.CreateActivityCallRequest{CustomerId: f.customer.ID.String(), Phone: "03", StatusId: f.statusID},
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "異常系/contact_id が不正",
			ctx:  testutil.AuthContext(t, f.userID, "ok@test.com"),
			req: func() *activityv1.CreateActivityCallRequest {
				bad := "not-uuid"
				return &activityv1.CreateActivityCallRequest{
					CustomerId: f.customer.ID.String(), Phone: "03", StatusId: f.statusID, ContactId: &bad,
				}
			}(),
			wantCode: connect.CodeInvalidArgument, wantErr: "invalid contact_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := f.svc.CreateActivityCall(tt.ctx, connect.NewRequest(tt.req))
			if tt.wantCode != 0 {
				requireConnectError(t, err, tt.wantCode, tt.wantErr)
			} else {
				require.NoError(t, err)
				if tt.checkResp != nil {
					tt.checkResp(t, resp.Msg)
				}
			}
		})
	}
}

// ─── CreateActivityEmailSent ────────────────────────────────────

func TestCreateActivityEmailSent(t *testing.T) {
	f := newFixture(t)

	tests := []struct {
		name     string
		email    string
		wantCode connect.Code
	}{
		{"異常系/email_claim が空 → FailedPrecondition", "", connect.CodeFailedPrecondition},
		{"異常系/mailClient が nil → Unavailable", "has@test.com", connect.CodeUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutil.AuthContext(t, f.userID, tt.email)
			_, err := f.svc.CreateActivityEmailSent(ctx, connect.NewRequest(&activityv1.CreateActivityEmailSentRequest{
				CustomerId: f.customer.ID.String(), MailTo: "to@x.com", Subject: "s", Body: "b",
			}))
			requireConnectError(t, err, tt.wantCode, "")
		})
	}
}

// ─── ListActivitiesByCustomerID ─────────────────────────────────

func TestListActivitiesByCustomerID(t *testing.T) {
	f := newFixture(t)
	ctx := testutil.AuthContext(t, f.userID, "list@test.com")

	t.Run("正常系/作成した Activity が一覧に含まれる", func(t *testing.T) {
		_, err := f.svc.CreateActivityCall(ctx, connect.NewRequest(&activityv1.CreateActivityCallRequest{
			CustomerId: f.customer.ID.String(), Phone: "090-list", StatusId: f.statusID,
		}))
		require.NoError(t, err)

		resp, err := f.svc.ListActivitiesByCustomerID(ctx, connect.NewRequest(&activityv1.ListActivitiesByCustomerIDRequest{
			CustomerId: f.customer.ID.String(),
		}))
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.Msg.Activities), 1)
	})

	t.Run("異常系/不正な UUID", func(t *testing.T) {
		_, err := f.svc.ListActivitiesByCustomerID(ctx, connect.NewRequest(&activityv1.ListActivitiesByCustomerIDRequest{
			CustomerId: "invalid",
		}))
		requireConnectError(t, err, connect.CodeInvalidArgument, "")
	})

	t.Run("異常系/存在しない customer", func(t *testing.T) {
		_, err := f.svc.ListActivitiesByCustomerID(ctx, connect.NewRequest(&activityv1.ListActivitiesByCustomerIDRequest{
			CustomerId: "00000000-0000-0000-0000-999999999999",
		}))
		requireConnectError(t, err, connect.CodeNotFound, "")
	})
}

// ─── UpdateActivityStatus ───────────────────────────────────────

func TestUpdateActivityStatus(t *testing.T) {
	f := newFixture(t)
	ctx := testutil.AuthContext(t, f.userID, "upd@test.com")

	created, err := f.svc.CreateActivityCall(ctx, connect.NewRequest(&activityv1.CreateActivityCallRequest{
		CustomerId: f.customer.ID.String(), Phone: "03-upd", StatusId: f.statusID,
	}))
	require.NoError(t, err)
	aid := created.Msg.Activity.Id

	t.Run("正常系/ステータスを更新できる", func(t *testing.T) {
		resp, err := f.svc.UpdateActivityStatus(ctx, connect.NewRequest(&activityv1.UpdateActivityStatusRequest{
			Id: aid, StatusId: f.statusID,
		}))
		require.NoError(t, err)
		assert.Equal(t, aid, resp.Msg.Activity.Id)
	})

	t.Run("異常系/存在しない Activity", func(t *testing.T) {
		_, err := f.svc.UpdateActivityStatus(ctx, connect.NewRequest(&activityv1.UpdateActivityStatusRequest{
			Id: "00000000-0000-0000-0000-000000000000", StatusId: f.statusID,
		}))
		require.Error(t, err)
	})

	t.Run("異常系/不正な UUID", func(t *testing.T) {
		_, err := f.svc.UpdateActivityStatus(ctx, connect.NewRequest(&activityv1.UpdateActivityStatusRequest{
			Id: "bad", StatusId: f.statusID,
		}))
		requireConnectError(t, err, connect.CodeInvalidArgument, "")
	})
}

// ─── helper ─────────────────────────────────────────────────────

func requireConnectError(t *testing.T, err error, code connect.Code, contains string) {
	t.Helper()
	require.Error(t, err)
	var connErr *connect.Error
	if code != 0 && assert.ErrorAs(t, err, &connErr) {
		assert.Equal(t, code, connErr.Code())
	}
	if contains != "" {
		assert.Contains(t, err.Error(), contains)
	}
}
