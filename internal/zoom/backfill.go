package zoom

import (
	"context"
	"errors"
	"fmt"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
)

// BackfillStats は backfill 実行結果のサマリ。CronJob ログで進捗を追うのに使う。
type BackfillStats struct {
	CallLogsFetched   int
	ActivitiesCreated int
	ActivitiesSkipped int // dedup / no-customer-match / other safe-skip
	RecordingsFetched int
	RecordingsArchived int
	Errors            int
}

// Backfiller は Zoom Phone API から過去の通話 + 録音を取得して
// Activity に upsert する。webhook が落ちて取りこぼした分や、
// webhook 以前の履歴を一括取り込みするのに使う。
//
// 流れ:
//  1. ListAccountCallLogs(from, to) で通話ログ全件取得
//  2. 各 call_log に対し、zoom_call_id で Activity を逆引き
//     - 既存あり → recording_url が空でかつ has_recording なら recording 補完
//     - 既存なし → Customer マッチ → Activity 作成 (HandleCallEnded と同じ流れ)
//  3. ListAccountRecordings(from, to) で録音 download_url を取得し、
//     Activity に未保存のものは S3 archive + recording_url 更新
//
// 冪等性:
//  - zoom_call_id UNIQUE 制約で 2 重作成を防ぐ
//  - recording_url は空 → 値あり の片方向更新のみ (上書きしない)
type Backfiller struct {
	client        *Client
	queries       *db.Queries
	archiver      *RecordingArchiver
	defaultUserID string
	staffNumbers  map[string]struct{} // 起動時にキャッシュ済の staff 番号集合
}

// NewBackfiller は Backfiller を組み立てる。client / queries が nil なら
// 機能 disabled な nil を返す (caller 側で if backfiller != nil チェック)。
func NewBackfiller(
	client *Client,
	queries *db.Queries,
	archiver *RecordingArchiver,
	defaultUserID string,
) *Backfiller {
	if client == nil || queries == nil {
		return nil
	}
	if defaultUserID == "" {
		defaultUserID = "system"
	}
	return &Backfiller{
		client:        client,
		queries:       queries,
		archiver:      archiver,
		defaultUserID: defaultUserID,
		staffNumbers:  map[string]struct{}{},
	}
}

// SetStaffNumbers は staff 内線番号集合を入れる (ActivityHandler と同じ用途)。
func (b *Backfiller) SetStaffNumbers(numbers []string) {
	b.staffNumbers = make(map[string]struct{}, len(numbers))
	for _, n := range numbers {
		if d := PhoneToDigits(n); d != "" {
			b.staffNumbers[d] = struct{}{}
		}
	}
}

// Run は from→to の期間で backfill を実行する。
//
// 期間は YYYY-MM-DD 形式 (Zoom API の制約に合わせる)。最大 1 ヶ月まで。
// 長い期間を一括で渡したいなら、呼び出し側で月ごとに分割して連続呼び出し。
func (b *Backfiller) Run(ctx context.Context, from, to string) (*BackfillStats, error) {
	if b == nil || b.client == nil {
		return nil, errors.New("backfill: client not configured")
	}
	stats := &BackfillStats{}

	// === 1) call_logs を全件取得 ===
	logs, err := b.client.ListAccountCallLogs(from, to)
	if err != nil {
		return stats, fmt.Errorf("backfill: list call_logs: %w", err)
	}
	stats.CallLogsFetched = len(logs)
	log.Info().Str("from", from).Str("to", to).Int("count", len(logs)).Msg("backfill: call_logs fetched")

	// === 2) 各 call_log を Activity に upsert ===
	for _, cl := range logs {
		if cl.CallID == "" {
			stats.ActivitiesSkipped++
			continue
		}
		if err := b.upsertActivityFromCallLog(ctx, cl, stats); err != nil {
			log.Warn().Err(err).Str("call_id", cl.CallID).Msg("backfill: upsert activity failed")
			stats.Errors++
		}
	}

	// === 3) recording 補完 ===
	recs, err := b.client.ListAccountRecordings(from, to)
	if err != nil {
		// recording 取得失敗は backfill 全体を fail させない (call_log 経路で
		// Activity は作れている)。
		log.Warn().Err(err).Msg("backfill: list recordings failed (proceeding without recordings)")
		return stats, nil
	}
	stats.RecordingsFetched = len(recs)
	log.Info().Int("count", len(recs)).Msg("backfill: recordings fetched")

	for _, rec := range recs {
		if err := b.attachRecordingToActivity(ctx, rec, stats); err != nil {
			log.Warn().Err(err).Str("call_id", rec.CallID).Msg("backfill: attach recording failed")
			stats.Errors++
		}
	}

	return stats, nil
}

// upsertActivityFromCallLog は単一 call_log を Activity 化する。既存 Activity
// と zoom_call_id でマッチしたら no-op (= dedup)。Customer マッチに失敗した
// ものは ActivitiesSkipped でカウントしてスキップ (CRM に未登録の番号)。
func (b *Backfiller) upsertActivityFromCallLog(ctx context.Context, cl CallLog, stats *BackfillStats) error {
	// dedup
	if _, err := b.queries.GetActivityByZoomCallID(ctx, pgtype.Text{String: cl.CallID, Valid: true}); err == nil {
		stats.ActivitiesSkipped++
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get activity by zoom_call_id: %w", err)
	}

	// 客側番号を選ぶ — call_log は did_number が E.164 (+81...)。
	customerPhone, customerName := b.pickCustomerSideFromCallLog(cl)
	if customerPhone == "" {
		stats.ActivitiesSkipped++
		return nil
	}

	occurredAt := parseZoomTime(cl.CallEndTime, cl.DateTime)

	match, err := MatchCustomerByPhoneAndTime(ctx, b.queries, customerPhone, occurredAt, 30*24*time.Hour)
	if err != nil {
		if errors.Is(err, ErrNoMatch) {
			log.Debug().
				Str("call_id", cl.CallID).
				Str("phone", customerPhone).
				Str("name", customerName).
				Msg("backfill: no customer match, skipping")
			stats.ActivitiesSkipped++
			return nil
		}
		return fmt.Errorf("match customer: %w", err)
	}

	contactID := pgtype.UUID{Valid: false}
	if match.ContactID != uuid.Nil {
		contactID = pgtype.UUID{Bytes: match.ContactID, Valid: true}
	}

	defaultStatus, err := b.queries.GetDefaultStatusByBookID(ctx, match.BookID)
	if err != nil {
		return fmt.Errorf("get default status: %w", err)
	}

	params := db.CreateActivityParams{
		ID:              uuid.New(),
		CustomerID:      match.CustomerID,
		ContactID:       contactID,
		Type:            "call",
		UserID:          b.defaultUserID,
		StatusID:        pgtype.UUID{Bytes: defaultStatus.ID, Valid: true},
		Phone:           pgtype.Text{String: JapanLocalPhone(customerPhone), Valid: true},
		MailFrom:        pgtype.Text{Valid: false},
		MailTo:          pgtype.Text{Valid: false},
		MailCc:          pgtype.Text{Valid: false},
		Subject:         pgtype.Text{Valid: false},
		Body:            pgtype.Text{Valid: false},
		MessageID:       pgtype.Text{Valid: false},
		OccurredAt:      occurredAt,
		DurationSeconds: pgtype.Int4{Int32: int32(cl.Duration), Valid: cl.Duration > 0},
		RecordingUrl:    pgtype.Text{Valid: false}, // step 3 で recording_url を埋める
		ZoomCallID:      pgtype.Text{String: cl.CallID, Valid: true},
	}
	if _, err := b.queries.CreateActivity(ctx, params); err != nil {
		return fmt.Errorf("insert activity: %w", err)
	}
	stats.ActivitiesCreated++
	log.Info().
		Str("call_id", cl.CallID).
		Str("customer", match.Name).
		Str("phone", JapanLocalPhone(customerPhone)).
		Int("duration", cl.Duration).
		Msg("backfill: activity created")
	return nil
}

// attachRecordingToActivity は recording の download_url を S3 に保存し、
// 紐づく Activity の recording_url を更新する。Activity がまだ無い場合や
// archiver disabled の場合はスキップ。
func (b *Backfiller) attachRecordingToActivity(ctx context.Context, rec Recording, stats *BackfillStats) error {
	if rec.CallID == "" || rec.DownloadURL == "" {
		return nil
	}
	if !b.archiver.Enabled() {
		return nil
	}

	// 既存 Activity 確認 (recording 専用 backfill モードでも、call_log 経由で
	// 直前に作っているはず)。
	act, err := b.queries.GetActivityByZoomCallID(ctx, pgtype.Text{String: rec.CallID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// call_logs ページからは漏れたが recordings ページに居る、という
			// レアケース。ここでは新規作成しない (call_log 経路に責任を持たせる)。
			log.Debug().Str("call_id", rec.CallID).Msg("backfill: recording without activity, skipping")
			return nil
		}
		return fmt.Errorf("get activity: %w", err)
	}
	if act.RecordingUrl.Valid && act.RecordingUrl.String != "" {
		// 既に archive 済 (webhook で取れていた場合など)
		return nil
	}

	s3path, err := b.archiver.Archive(ctx, rec.CallID, rec.DownloadURL)
	if err != nil {
		return fmt.Errorf("archive recording: %w", err)
	}
	if err := b.queries.UpdateActivityRecordingURL(ctx, db.UpdateActivityRecordingURLParams{
		ZoomCallID:   pgtype.Text{String: rec.CallID, Valid: true},
		RecordingUrl: pgtype.Text{String: s3path, Valid: true},
	}); err != nil {
		return fmt.Errorf("update recording_url: %w", err)
	}
	stats.RecordingsArchived++
	log.Info().Str("call_id", rec.CallID).Str("s3_path", s3path).Msg("backfill: recording archived")
	return nil
}

// pickCustomerSideFromCallLog は call_log entry から客側の番号 + 表示名を返す。
// did_number が E.164 で来るのでそれを基準に判定。
func (b *Backfiller) pickCustomerSideFromCallLog(cl CallLog) (phone, name string) {
	callerDigits := PhoneToDigits(cl.CallerDIDNumber)
	calleeDigits := PhoneToDigits(cl.CalleeDIDNumber)
	_, callerStaff := b.staffNumbers[callerDigits]
	_, calleeStaff := b.staffNumbers[calleeDigits]

	if len(b.staffNumbers) > 0 {
		if !callerStaff && calleeStaff {
			return cl.CallerDIDNumber, cl.CallerName
		}
		if callerStaff && !calleeStaff {
			return cl.CalleeDIDNumber, cl.CalleeName
		}
	}
	switch cl.Direction {
	case "inbound":
		return cl.CallerDIDNumber, cl.CallerName
	case "outbound":
		return cl.CalleeDIDNumber, cl.CalleeName
	}
	return cl.CalleeDIDNumber, cl.CalleeName
}
