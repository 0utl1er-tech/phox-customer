package activity_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/activity"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ─── fixture ────────────────────────────────────────────────────

type bookStatsFixture struct {
	svc       *activity.ActivityService
	queries   *db.Queries
	book      db.Book
	customerA db.Customer
	customerB db.Customer
	userX     db.User // owner
	userY     db.User // editor (permit 付与済み)
	statusID  uuid.UUID
}

func newBookStatsFixture(t *testing.T) bookStatsFixture {
	t.Helper()
	_, q := testutil.SetupTestDB(t)
	ctx := context.Background()
	cid := testutil.TestCompanyID(t, q)
	ux := testutil.TestUser(t, q, "bfs-x-"+t.Name(), cid)
	uy := testutil.TestUser(t, q, "bfs-y-"+t.Name(), cid)
	b := testutil.TestBook(t, q, ux.ID)
	_, err := q.CreatePermit(ctx, db.CreatePermitParams{
		ID: uuid.New(), BookID: b.ID, UserID: uy.ID, Role: db.RoleEditor,
	})
	require.NoError(t, err)
	ca := testutil.TestCustomer(t, q, b.ID)
	cb := testutil.TestCustomer(t, q, b.ID)
	s, err := q.GetDefaultStatusByBookID(ctx, b.ID)
	require.NoError(t, err)
	return bookStatsFixture{
		svc: activity.NewActivityService(q, nil, nil), queries: q,
		book: b, customerA: ca, customerB: cb, userX: ux, userY: uy, statusID: s.ID,
	}
}

// seedActivity は型・担当者・時刻を制御して Activity を直接 insert する。
func (f bookStatsFixture) seedActivity(t *testing.T, typ string, customerID uuid.UUID, userID string, statusID *uuid.UUID, occurredAt time.Time) {
	t.Helper()
	params := db.CreateActivityParams{
		ID:         uuid.New(),
		CustomerID: customerID,
		Type:       typ,
		UserID:     userID,
		OccurredAt: occurredAt,
	}
	if statusID != nil {
		params.StatusID = pgtype.UUID{Bytes: *statusID, Valid: true}
	}
	if typ != "call" {
		params.MailFrom = pgtype.Text{String: "from@example.com", Valid: true}
		params.MailTo = pgtype.Text{String: "to@example.com", Valid: true}
		params.Subject = pgtype.Text{String: "件名", Valid: true}
		params.MessageID = pgtype.Text{String: "msg-" + uuid.NewString(), Valid: true}
	} else {
		params.Phone = pgtype.Text{String: "090-0000-0000", Valid: true}
	}
	_, err := f.queries.CreateActivity(context.Background(), params)
	require.NoError(t, err)
}

func ts(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }

var statsBase = time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

// ─── ListActivitiesByBookID ─────────────────────────────────────

func TestListActivitiesByBookID(t *testing.T) {
	f := newBookStatsFixture(t)
	ctx := testutil.AuthContext(t, f.userX.ID, "x@test.com")

	// customerA: call(X), email_sent(Y) / customerB: call(Y)
	f.seedActivity(t, "call", f.customerA.ID, f.userX.ID, &f.statusID, statsBase)
	f.seedActivity(t, "email_sent", f.customerA.ID, f.userY.ID, nil, statsBase.Add(1*time.Hour))
	f.seedActivity(t, "call", f.customerB.ID, f.userY.ID, &f.statusID, statsBase.Add(2*time.Hour))

	t.Run("全件_降順_顧客名つき", func(t *testing.T) {
		resp, err := f.svc.ListActivitiesByBookID(ctx, connect.NewRequest(&activityv1.ListActivitiesByBookIDRequest{
			BookId: f.book.ID.String(),
		}))
		require.NoError(t, err)
		require.Len(t, resp.Msg.Activities, 3)
		assert.EqualValues(t, 3, resp.Msg.TotalCount)
		// occurred_at 降順
		assert.Equal(t, f.customerB.ID.String(), resp.Msg.Activities[0].CustomerId)
		// Book 横断フィードでは顧客名が埋まる
		require.NotNil(t, resp.Msg.Activities[0].CustomerName)
		assert.Equal(t, f.customerB.Name, *resp.Msg.Activities[0].CustomerName)
	})

	t.Run("type絞り込み", func(t *testing.T) {
		resp, err := f.svc.ListActivitiesByBookID(ctx, connect.NewRequest(&activityv1.ListActivitiesByBookIDRequest{
			BookId: f.book.ID.String(),
			Types:  []activityv1.ActivityType{activityv1.ActivityType_ACTIVITY_TYPE_EMAIL_SENT},
		}))
		require.NoError(t, err)
		require.Len(t, resp.Msg.Activities, 1)
		assert.EqualValues(t, 1, resp.Msg.TotalCount)
	})

	t.Run("担当者絞り込み", func(t *testing.T) {
		resp, err := f.svc.ListActivitiesByBookID(ctx, connect.NewRequest(&activityv1.ListActivitiesByBookIDRequest{
			BookId: f.book.ID.String(),
			UserId: &f.userY.ID,
		}))
		require.NoError(t, err)
		assert.Len(t, resp.Msg.Activities, 2)
	})

	t.Run("期間絞り込み", func(t *testing.T) {
		resp, err := f.svc.ListActivitiesByBookID(ctx, connect.NewRequest(&activityv1.ListActivitiesByBookIDRequest{
			BookId:       f.book.ID.String(),
			OccurredFrom: ts(statsBase.Add(30 * time.Minute)),
			OccurredTo:   ts(statsBase.Add(90 * time.Minute)),
		}))
		require.NoError(t, err)
		require.Len(t, resp.Msg.Activities, 1)
		assert.Equal(t, activityv1.ActivityType_ACTIVITY_TYPE_EMAIL_SENT, resp.Msg.Activities[0].Type)
	})

	t.Run("ページネーション_totalは全件", func(t *testing.T) {
		resp, err := f.svc.ListActivitiesByBookID(ctx, connect.NewRequest(&activityv1.ListActivitiesByBookIDRequest{
			BookId: f.book.ID.String(),
			Limit:  1,
			Offset: 1,
		}))
		require.NoError(t, err)
		assert.Len(t, resp.Msg.Activities, 1)
		assert.EqualValues(t, 3, resp.Msg.TotalCount)
	})

	t.Run("permitなしはPermissionDenied", func(t *testing.T) {
		_, q := testutil.SetupTestDB(t)
		outsider := testutil.TestUser(t, q, "bfs-outsider", testutil.TestCompanyID(t, q))
		octx := testutil.AuthContext(t, outsider.ID, "o@test.com")
		_, err := f.svc.ListActivitiesByBookID(octx, connect.NewRequest(&activityv1.ListActivitiesByBookIDRequest{
			BookId: f.book.ID.String(),
		}))
		require.Error(t, err)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})

	t.Run("不正なbook_idはInvalidArgument", func(t *testing.T) {
		_, err := f.svc.ListActivitiesByBookID(ctx, connect.NewRequest(&activityv1.ListActivitiesByBookIDRequest{
			BookId: "broken",
		}))
		require.Error(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})
}

// ─── GetCallStats ───────────────────────────────────────────────

func TestGetCallStats(t *testing.T) {
	f := newBookStatsFixture(t)
	ctx := testutil.AuthContext(t, f.userX.ID, "x@test.com")

	// X: status あり×2 + status なし×1 / Y: status あり×1
	f.seedActivity(t, "call", f.customerA.ID, f.userX.ID, &f.statusID, statsBase)
	f.seedActivity(t, "call", f.customerB.ID, f.userX.ID, &f.statusID, statsBase.Add(time.Hour))
	f.seedActivity(t, "call", f.customerA.ID, f.userX.ID, nil, statsBase.Add(2*time.Hour))
	f.seedActivity(t, "call", f.customerA.ID, f.userY.ID, &f.statusID, statsBase.Add(3*time.Hour))
	// 期間外 (除外されること)
	f.seedActivity(t, "call", f.customerA.ID, f.userY.ID, &f.statusID, statsBase.Add(-24*time.Hour))
	// email は集計対象外
	f.seedActivity(t, "email_sent", f.customerA.ID, f.userX.ID, nil, statsBase)

	resp, err := f.svc.GetCallStats(ctx, connect.NewRequest(&activityv1.GetCallStatsRequest{
		BookId:       f.book.ID.String(),
		OccurredFrom: ts(statsBase),
		OccurredTo:   ts(statsBase.Add(24 * time.Hour)),
	}))
	require.NoError(t, err)

	// セルを (user, status有無) で索引化して検証
	type key struct {
		userID    string
		hasStatus bool
	}
	got := map[key]int64{}
	for _, c := range resp.Msg.Cells {
		got[key{c.UserId, c.StatusId != nil}] = c.Count
	}
	assert.EqualValues(t, 2, got[key{f.userX.ID, true}], "X の status ありコール")
	assert.EqualValues(t, 1, got[key{f.userX.ID, false}], "X の status なしコール")
	assert.EqualValues(t, 1, got[key{f.userY.ID, true}], "Y の status ありコール (期間外は除外)")
}

// ─── GetMailStats ───────────────────────────────────────────────

func TestGetMailStats(t *testing.T) {
	f := newBookStatsFixture(t)
	ctx := testutil.AuthContext(t, f.userX.ID, "x@test.com")

	// X が customerA に送信 ×2 → customerA から受信 ×1 (X に帰属)
	f.seedActivity(t, "email_sent", f.customerA.ID, f.userX.ID, nil, statsBase)
	f.seedActivity(t, "email_sent", f.customerA.ID, f.userX.ID, nil, statsBase.Add(time.Hour))
	f.seedActivity(t, "email_received", f.customerA.ID, f.userY.ID, nil, statsBase.Add(2*time.Hour))
	// customerB からは先行送信なしで受信 → 帰属不明 (user_id="")
	f.seedActivity(t, "email_received", f.customerB.ID, f.userY.ID, nil, statsBase.Add(3*time.Hour))
	// Y が customerB に送信 (受信の後なので帰属しない)
	f.seedActivity(t, "email_sent", f.customerB.ID, f.userY.ID, nil, statsBase.Add(4*time.Hour))

	resp, err := f.svc.GetMailStats(ctx, connect.NewRequest(&activityv1.GetMailStatsRequest{
		BookId: f.book.ID.String(),
	}))
	require.NoError(t, err)

	byUser := map[string]*activityv1.MailStatsRow{}
	for _, r := range resp.Msg.Rows {
		byUser[r.UserId] = r
	}

	require.Contains(t, byUser, f.userX.ID)
	assert.EqualValues(t, 2, byUser[f.userX.ID].SentCount)
	assert.EqualValues(t, 1, byUser[f.userX.ID].ReplyCount, "customerA の返信は X に帰属")

	require.Contains(t, byUser, f.userY.ID)
	assert.EqualValues(t, 1, byUser[f.userY.ID].SentCount)
	assert.EqualValues(t, 0, byUser[f.userY.ID].ReplyCount)

	require.Contains(t, byUser, "", "先行送信のない受信は帰属不明行")
	assert.EqualValues(t, 1, byUser[""].ReplyCount)
}
