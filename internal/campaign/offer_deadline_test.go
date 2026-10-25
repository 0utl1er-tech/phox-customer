package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// jstAt は JST の日時を作る (テスト用)。
func jstAt(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, jst)
}

func TestOfferDeadlineDate(t *testing.T) {
	cases := []struct {
		name    string
		sentAt  time.Time
		days    int
		want    string // YYYY-MM-DD (JST)
		covered bool
	}{
		{"平日に着地", jstAt(2026, 10, 26, 10), 21, "2026-11-16", true},
		{"土曜に着地 → 月曜", jstAt(2026, 10, 24, 10), 21, "2026-11-16", true},
		{"日曜に着地 → 月曜", jstAt(2026, 10, 25, 10), 21, "2026-11-16", true},
		{"文化の日 (火) → 水曜", jstAt(2026, 10, 13, 10), 21, "2026-11-04", true},
		{"勤労感謝の日 (月) → 火曜", jstAt(2026, 11, 2, 10), 21, "2026-11-24", true},
		{"元日 (金) → 土日も飛ばして月曜", jstAt(2026, 12, 11, 10), 21, "2027-01-04", true},
		{"振替休日 2027-03-22 → 火曜", jstAt(2027, 3, 1, 10), 21, "2027-03-23", true},
		{"振替休日 2026-05-06 → 木曜", jstAt(2026, 4, 15, 10), 21, "2026-05-07", true},
		{"連休 (5/3 日曜着地) → 5/7", jstAt(2026, 4, 12, 10), 21, "2026-05-07", true},
		{"国民の休日 9/22 → 秋分の日も飛ばして 9/24", jstAt(2026, 9, 1, 10), 21, "2026-09-24", true},
		// UTC 15:30 は JST の翌日 0:30 — 暦日は JST で数える。
		{"JST の日付で数える", time.Date(2026, 10, 25, 15, 30, 0, 0, time.UTC), 21, "2026-11-16", true},
		{"日数を変える", jstAt(2026, 10, 26, 10), 7, "2026-11-02", true},
		// 2028-01-10 は成人の日の見込みだが表に無い — 土日だけ飛ばして covered=false。
		{"表の範囲外", jstAt(2027, 12, 20, 10), 21, "2028-01-10", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, covered := OfferDeadlineDate(tc.sentAt, tc.days)
			if s := got.In(jst).Format("2006-01-02"); s != tc.want {
				t.Fatalf("OfferDeadlineDate(%s, %d) = %s, want %s", tc.sentAt, tc.days, s, tc.want)
			}
			if covered != tc.covered {
				t.Fatalf("covered = %v, want %v", covered, tc.covered)
			}
		})
	}
}

func TestFormatJPMonthDayWeekday(t *testing.T) {
	cases := map[string]time.Time{
		"11月16日（月）": jstAt(2026, 11, 16, 0),
		"11月14日（土）": jstAt(2026, 11, 14, 0),
		"1月4日（月）":   jstAt(2027, 1, 4, 0),
		"3月23日（火）":  jstAt(2027, 3, 23, 0),
		// UTC で前日でも JST の日付・曜日で出す。
		"11月15日（日）": time.Date(2026, 11, 14, 16, 0, 0, 0, time.UTC),
	}
	for want, in := range cases {
		if got := FormatJPMonthDayWeekday(in); got != want {
			t.Errorf("FormatJPMonthDayWeekday(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestOfferDeadlineDaysFromEnv(t *testing.T) {
	t.Setenv(offerDeadlineDaysEnv, "")
	require.Equal(t, 21, offerDeadlineDaysFromEnv())
	t.Setenv(offerDeadlineDaysEnv, "14")
	require.Equal(t, 14, offerDeadlineDaysFromEnv())
	for _, bad := range []string{"abc", "0", "-3"} {
		t.Setenv(offerDeadlineDaysEnv, bad)
		require.Equal(t, 21, offerDeadlineDaysFromEnv(), "value %q", bad)
	}
}

func TestJPHolidayTable(t *testing.T) {
	// 2026・2027 とも 振替休日・国民の休日を含めて内閣府 CSV と同数。
	per := map[int]int{}
	for k := range jpHolidays {
		d, err := time.ParseInLocation("2006-01-02", k, jst)
		require.NoError(t, err, k)
		per[d.Year()]++
		if wd := d.Weekday(); k != "2026-05-03" && k != "2027-03-21" && (wd == time.Saturday || wd == time.Sunday) {
			// 日曜の祝日は振替休日とセットの 2 件だけのはず。
			t.Errorf("unexpected weekend holiday %s (%s)", k, wd)
		}
	}
	require.Equal(t, map[int]int{2026: 18, 2027: 17}, per)
	_, h, ok := jpHoliday(jstAt(2026, 11, 3, 0))
	require.True(t, h && ok)
	_, h, ok = jpHoliday(jstAt(2026, 11, 4, 0))
	require.True(t, !h && ok)
	_, _, ok = jpHoliday(jstAt(2028, 1, 10, 0))
	require.False(t, ok)
}

// fakeFieldSetter は SetCustomerCustomFieldIfAbsent の呼び出しを記録する。
type fakeFieldSetter struct {
	calls  []db.SetCustomerCustomFieldIfAbsentParams
	stored string // "" なら引数の値をそのまま返す
	err    error
}

func (f *fakeFieldSetter) SetCustomerCustomFieldIfAbsent(_ context.Context, arg db.SetCustomerCustomFieldIfAbsentParams) (string, error) {
	f.calls = append(f.calls, arg)
	if f.err != nil {
		return "", f.err
	}
	if f.stored != "" {
		return f.stored, nil
	}
	return arg.Value, nil
}

func TestEnsureOfferDeadline(t *testing.T) {
	ctx := context.Background()
	now := jstAt(2026, 10, 26, 10) // → 11月16日（月）
	const want = "11月16日（月）"
	const key = FieldsPrefix + OfferDeadlineKey
	withRef := db.Campaign{ID: uuid.New(), Subject: "件名", Body: "{{fields.offer_deadline}}までにお申し込みを"}
	stepRef := []db.CampaignStep{{StepNo: 2, Body: "期限は{{ fields.offer_deadline }}です"}}
	noRef := db.Campaign{ID: uuid.New(), Subject: "件名", Body: "{{customer_name}} 様 {{fields.meo_score}}"}
	custID := uuid.New()

	t.Run("1 通目で値が無ければ決めて保存して差し込む", func(t *testing.T) {
		f := &fakeFieldSetter{}
		vars := map[string]string{}
		ensureOfferDeadline(ctx, f, withRef, nil, 1, custID, vars, now, 21)
		require.Equal(t, want, vars[key])
		require.Len(t, f.calls, 1)
		require.Equal(t, db.SetCustomerCustomFieldIfAbsentParams{Key: "offer_deadline", Value: want, ID: custID}, f.calls[0])
	})

	t.Run("ステップ側だけが参照していても対象", func(t *testing.T) {
		f := &fakeFieldSetter{}
		vars := map[string]string{}
		c := withRef
		c.Body = "本文"
		ensureOfferDeadline(ctx, f, c, stepRef, 1, custID, vars, now, 21)
		require.Equal(t, want, vars[key])
		require.Len(t, f.calls, 1)
	})

	t.Run("既存値は上書きしない", func(t *testing.T) {
		f := &fakeFieldSetter{}
		vars := map[string]string{key: "11月10日（火）"}
		ensureOfferDeadline(ctx, f, withRef, stepRef, 1, custID, vars, now, 21)
		require.Equal(t, "11月10日（火）", vars[key])
		require.Empty(t, f.calls)
	})

	t.Run("参照していないキャンペーンでは何もしない", func(t *testing.T) {
		f := &fakeFieldSetter{}
		vars := map[string]string{}
		ensureOfferDeadline(ctx, f, noRef, []db.CampaignStep{{StepNo: 2, Body: "続き"}}, 1, custID, vars, now, 21)
		_, ok := vars[key]
		require.False(t, ok)
		require.Empty(t, f.calls)
	})

	t.Run("2 通目以降は再計算しない", func(t *testing.T) {
		f := &fakeFieldSetter{}
		vars := map[string]string{}
		ensureOfferDeadline(ctx, f, withRef, stepRef, 2, custID, vars, now, 21)
		require.Empty(t, vars[key])
		require.Empty(t, f.calls)
	})

	t.Run("保存に失敗しても計算値を差し込む", func(t *testing.T) {
		f := &fakeFieldSetter{err: errors.New("db down")}
		vars := map[string]string{}
		ensureOfferDeadline(ctx, f, withRef, nil, 1, custID, vars, now, 21)
		require.Equal(t, want, vars[key])
	})

	t.Run("先に別の値が保存されていたらそちらに揃える", func(t *testing.T) {
		f := &fakeFieldSetter{stored: "11月9日（月）"}
		vars := map[string]string{}
		ensureOfferDeadline(ctx, f, withRef, nil, 1, custID, vars, now, 21)
		require.Equal(t, "11月9日（月）", vars[key])
	})

	t.Run("差し込み結果", func(t *testing.T) {
		vars := map[string]string{}
		ensureOfferDeadline(ctx, &fakeFieldSetter{}, withRef, nil, 1, custID, vars, now, 21)
		require.Equal(t, "11月16日（月）までにお申し込みを", Render(withRef.Body, vars))
	})
}

// TestSetCustomerCustomFieldIfAbsent は保存クエリの DB 実測テスト
// (TEST_DB_SOURCE に migrate 済みの Postgres が無ければ skip)。
func TestSetCustomerCustomFieldIfAbsent(t *testing.T) {
	ctx := context.Background()
	pool, q := testutil.SetupTestDB(t)

	book, err := q.CreateBook(ctx, db.CreateBookParams{ID: uuid.New(), Name: "OD" + uuid.NewString()[:8]})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM "Customer" WHERE book_id = $1`, book.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "Book" WHERE id = $1`, book.ID)
	})
	mk := func(fields string) uuid.UUID {
		id := uuid.New()
		_, err := q.CreateCustomer(ctx, db.CreateCustomerParams{
			ID: id, BookID: book.ID, Name: "期限テスト", CustomFields: []byte(fields),
		})
		require.NoError(t, err)
		return id
	}
	fields := func(id uuid.UUID) map[string]string {
		c, err := q.GetCustomer(ctx, id)
		require.NoError(t, err)
		var m map[string]string
		require.NoError(t, json.Unmarshal(c.CustomFields, &m))
		return m
	}

	// 無ければ足す。他のキーは残す。updated_at は動かさない。
	id := mk(`{"meo_score":"42","note":"改行\nあり"}`)
	before, err := q.GetCustomer(ctx, id)
	require.NoError(t, err)
	got, err := q.SetCustomerCustomFieldIfAbsent(ctx, db.SetCustomerCustomFieldIfAbsentParams{
		Key: OfferDeadlineKey, Value: "11月16日（月）", ID: id,
	})
	require.NoError(t, err)
	require.Equal(t, "11月16日（月）", got)
	require.Equal(t, map[string]string{"meo_score": "42", "note": "改行\nあり", "offer_deadline": "11月16日（月）"}, fields(id))
	after, err := q.GetCustomer(ctx, id)
	require.NoError(t, err)
	require.True(t, before.UpdatedAt.Equal(after.UpdatedAt), "updated_at changed")

	// 2 回目は別の値を渡しても上書きせず、保存済みの値を返す。
	got, err = q.SetCustomerCustomFieldIfAbsent(ctx, db.SetCustomerCustomFieldIfAbsentParams{
		Key: OfferDeadlineKey, Value: "11月30日（月）", ID: id,
	})
	require.NoError(t, err)
	require.Equal(t, "11月16日（月）", got)
	require.Equal(t, "11月16日（月）", fields(id)["offer_deadline"])

	// 空文字は「無い」扱いで埋める。
	id2 := mk(`{"offer_deadline":""}`)
	got, err = q.SetCustomerCustomFieldIfAbsent(ctx, db.SetCustomerCustomFieldIfAbsentParams{
		Key: OfferDeadlineKey, Value: "11月16日（月）", ID: id2,
	})
	require.NoError(t, err)
	require.Equal(t, "11月16日（月）", got)

	// custom_fields が空 ({}) でも足せる。
	id3 := mk(`{}`)
	got, err = q.SetCustomerCustomFieldIfAbsent(ctx, db.SetCustomerCustomFieldIfAbsentParams{
		Key: OfferDeadlineKey, Value: "11月16日（月）", ID: id3,
	})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"offer_deadline": "11月16日（月）"}, fields(id3))

	// 無い顧客は ErrNoRows (呼び出し側は warning を出して計算値で送る)。
	_, err = q.SetCustomerCustomFieldIfAbsent(ctx, db.SetCustomerCustomFieldIfAbsentParams{
		Key: OfferDeadlineKey, Value: "x", ID: uuid.New(),
	})
	require.Error(t, err)
}
