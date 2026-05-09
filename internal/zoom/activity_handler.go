package zoom

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
)

// ActivityHandler は Zoom Phone webhook イベントを Activity row に変換する。
// 責務:
//   - phone.callee_ended / phone.caller_ended → Activity 作成 (type='call')
//   - phone.recording_completed → Activity 更新 (recording_url セット)
//   - 内側ユーザー (= phox staff の Zoom Phone 番号) を初回 ListPhoneUsers
//     でキャッシュして以降は in-memory 参照
//
// 失敗パスは log + skip。Webhook を再送するメカニズムが Zoom 側に無いため
// (= 一度 200 を返すと再送されない)、致命エラー以外は飲み込んで OK 返す。
type ActivityHandler struct {
	queries  *db.Queries
	archiver *RecordingArchiver

	// 内側ユーザー (phox staff) の Zoom Phone 番号を保持。Activity の
	// Customer phone を decide する際、caller / callee のうちこのリストに
	// 入ってない方が客側、と判定する。空 set なら direction フィールドの
	// inbound/outbound を信じる動作にフォールバック。
	staffMu      sync.RWMutex
	staffNumbers map[string]struct{} // PhoneToDigits 後の 10 桁 string

	defaultUserID string // user_id="system" 等の seed user
}

// NewActivityHandler は handler を構築。
//   - queries:        sqlc クエリ
//   - archiver:       Recording archiver (nil 可、nil のとき recording 保存スキップ)
//   - defaultUserID:  user_id seed (IMAP worker と同じ "system")
func NewActivityHandler(
	queries *db.Queries,
	archiver *RecordingArchiver,
	defaultUserID string,
) *ActivityHandler {
	if defaultUserID == "" {
		defaultUserID = "system"
	}
	return &ActivityHandler{
		queries:       queries,
		archiver:      archiver,
		staffNumbers:  map[string]struct{}{},
		defaultUserID: defaultUserID,
	}
}

// SetStaffNumbers は内側ユーザー (phox staff) の Zoom Phone 番号集合を更新。
// startup 時に Zoom Client.ListPhoneUsers() の結果を渡す想定。
func (h *ActivityHandler) SetStaffNumbers(numbers []string) {
	set := make(map[string]struct{}, len(numbers))
	for _, n := range numbers {
		if d := PhoneToDigits(n); d != "" {
			set[d] = struct{}{}
		}
	}
	h.staffMu.Lock()
	h.staffNumbers = set
	h.staffMu.Unlock()
	log.Info().Int("staff_phone_count", len(set)).Msg("zoom: staff numbers cached")
}

// HandleCallEnded は phone.callee_ended / phone.caller_ended から Activity 作成。
//
// 流れ:
//  1. caller / callee のどちらが客側 (= staff 番号集合に居ない方) か判定。
//     staff 集合が空 / 両方 staff / 両方 non-staff の場合は direction
//     フィールド (inbound = caller が客 / outbound = callee が客) でフォールバック。
//  2. 客側番号で MatchCustomerByPhoneAndTime → Customer 確定
//  3. Activity 作成 (type='call', duration_seconds, zoom_call_id, occurred_at)
func (h *ActivityHandler) HandleCallEnded(event PhoneCallEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if event.CallID == "" {
		log.Warn().Msg("zoom: callee_ended without call_id, skipping")
		return
	}

	// dedup: 既に同 call_id の Activity があれば skip (Zoom が重複配信した時の保護)
	if existing, err := h.queries.GetActivityByZoomCallID(ctx, pgtype.Text{String: event.CallID, Valid: true}); err == nil {
		log.Debug().
			Str("call_id", event.CallID).
			Str("existing_activity_id", existing.ID.String()).
			Msg("zoom: callee_ended duplicate, skipping")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		log.Warn().Err(err).Str("call_id", event.CallID).Msg("zoom: dedup lookup failed")
		// 続行 — マッチ + insert で UNIQUE 違反になれば DB が止める
	}

	customerPhone, customerName := h.pickCustomerSide(event)
	if customerPhone == "" {
		log.Warn().
			Str("call_id", event.CallID).
			Str("caller", event.Caller.PhoneNumber).
			Str("callee", event.Callee.PhoneNumber).
			Msg("zoom: cannot identify customer side, skipping")
		return
	}

	occurredAt := parseZoomTime(event.CallEndTime, event.ConnectedStartTime, event.RingingStartTime)

	match, err := MatchCustomerByPhoneAndTime(
		ctx, h.queries, customerPhone, occurredAt, 30*24*time.Hour,
	)
	if err != nil {
		if errors.Is(err, ErrNoMatch) {
			log.Info().
				Str("call_id", event.CallID).
				Str("phone", customerPhone).
				Str("name", customerName).
				Msg("zoom: no customer matches phone, skipping ingestion (consider adding to CRM)")
			return
		}
		log.Warn().Err(err).Str("call_id", event.CallID).Msg("zoom: match failed")
		return
	}

	contactID := pgtype.UUID{Valid: false}
	if match.ContactID != uuid.Nil {
		contactID = pgtype.UUID{Bytes: match.ContactID, Valid: true}
	}

	// type='call' の Activity は DB 制約 activity_call_requires_status で
	// status_id NOT NULL を要求する。webhook 経路ではユーザーが status を選ぶ
	// 余地が無いので、Customer が属する Book のデフォルト status (priority=1)
	// を引いて使う。Phase 20b の backfill migration (000008) で全 Book に
	// 必ず1件 seed されているはずなので、NotFound はバグレベルのアサート。
	defaultStatus, err := h.queries.GetDefaultStatusByBookID(ctx, match.BookID)
	if err != nil {
		log.Warn().Err(err).
			Str("call_id", event.CallID).
			Str("book_id", match.BookID.String()).
			Msg("zoom: no default status for book, skipping (backfill migration may have missed this Book)")
		return
	}

	params := db.CreateActivityParams{
		ID:              uuid.New(),
		CustomerID:      match.CustomerID,
		ContactID:       contactID,
		Type:            "call",
		UserID:          h.defaultUserID,
		StatusID:        pgtype.UUID{Bytes: defaultStatus.ID, Valid: true},
		Phone:           pgtype.Text{String: customerPhone, Valid: true},
		MailFrom:        pgtype.Text{Valid: false},
		MailTo:          pgtype.Text{Valid: false},
		MailCc:          pgtype.Text{Valid: false},
		Subject:         pgtype.Text{Valid: false},
		Body:            pgtype.Text{Valid: false},
		MessageID:       pgtype.Text{Valid: false},
		OccurredAt:      occurredAt,
		DurationSeconds: pgtype.Int4{Int32: int32(event.DurationSec), Valid: event.DurationSec > 0},
		RecordingUrl:    pgtype.Text{Valid: false}, // recording_completed で後ほどセット
		ZoomCallID:      pgtype.Text{String: event.CallID, Valid: true},
	}

	if _, err := h.queries.CreateActivity(ctx, params); err != nil {
		log.Warn().Err(err).
			Str("call_id", event.CallID).
			Str("customer_id", match.CustomerID.String()).
			Msg("zoom: insert call activity")
		return
	}
	log.Info().
		Str("call_id", event.CallID).
		Str("customer_id", match.CustomerID.String()).
		Str("customer_name", match.Name).
		Str("phone", customerPhone).
		Int("duration_sec", event.DurationSec).
		Msg("zoom: call activity created")
}

// HandleRecordingCompleted は phone.recording_completed から Activity 更新。
// 流れ:
//  1. archiver が disabled なら DB 更新だけ skip (recording_url を Zoom URL
//     のまま保存する選択もあるが、30 日で消えるので原則 archive 必須)
//  2. download → S3 PUT (archiver.Archive)
//  3. UpdateActivityRecordingURL で zoom_call_id 検索 + recording_url 更新
//
// HandleCallEnded で Activity が先に作られている前提。recording_completed が
// callee_ended より先に来た場合 (= 異常順序) はログだけ出して skip — リトライ
// は Zoom 側が webhook を再送する仕組みが無いため不可。Phase 23 で polling
// 経路 (GetCallRecordings) を追加して救済する予定。
func (h *ActivityHandler) HandleRecordingCompleted(event RecordingCompletedEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if event.CallID == "" || event.DownloadURL == "" {
		log.Warn().Msg("zoom: recording_completed missing call_id or download_url")
		return
	}

	// Activity 存在確認 (callee_ended が来てない順序事故の検知)
	if _, err := h.queries.GetActivityByZoomCallID(ctx, pgtype.Text{String: event.CallID, Valid: true}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Warn().
				Str("call_id", event.CallID).
				Msg("zoom: recording_completed before callee_ended (no activity), skipping archive")
			return
		}
		log.Warn().Err(err).Str("call_id", event.CallID).Msg("zoom: recording lookup failed")
		return
	}

	if !h.archiver.Enabled() {
		log.Info().Str("call_id", event.CallID).Msg("zoom: recording archiver disabled, skipping")
		return
	}

	s3path, err := h.archiver.Archive(ctx, event.CallID, event.DownloadURL)
	if err != nil {
		log.Warn().Err(err).Str("call_id", event.CallID).Msg("zoom: recording archive failed")
		return
	}

	if err := h.queries.UpdateActivityRecordingURL(ctx, db.UpdateActivityRecordingURLParams{
		ZoomCallID:   pgtype.Text{String: event.CallID, Valid: true},
		RecordingUrl: pgtype.Text{String: s3path, Valid: true},
	}); err != nil {
		log.Warn().Err(err).Str("call_id", event.CallID).Msg("zoom: recording_url update failed")
		return
	}
	log.Info().
		Str("call_id", event.CallID).
		Str("s3_path", s3path).
		Msg("zoom: recording attached to activity")
}

// pickCustomerSide は caller/callee のうち客側の番号 + 表示名を返す。
// staff 番号集合があればそれで判定、無ければ direction フィールドで判定。
//
// staff numbers は phox 側の Zoom Phone ユーザー番号 (= 自社線)。客側 = それ
// 以外。両方 staff / 両方 non-staff の場合は direction で判定:
//   - inbound  → caller が客 (外から入電)
//   - outbound → callee が客 (こちらから架電)
//
// direction も無い場合は callee を客とみなす (大半のユースケースで合う)。
func (h *ActivityHandler) pickCustomerSide(e PhoneCallEvent) (phone, name string) {
	h.staffMu.RLock()
	staff := h.staffNumbers
	h.staffMu.RUnlock()

	callerDigits := PhoneToDigits(e.Caller.PhoneNumber)
	calleeDigits := PhoneToDigits(e.Callee.PhoneNumber)
	_, callerStaff := staff[callerDigits]
	_, calleeStaff := staff[calleeDigits]

	if len(staff) > 0 {
		if !callerStaff && calleeStaff {
			return e.Caller.PhoneNumber, e.Caller.Name
		}
		if callerStaff && !calleeStaff {
			return e.Callee.PhoneNumber, e.Callee.Name
		}
		// 両方 staff / 両方 non-staff → fallthrough to direction
	}

	switch strings.ToLower(e.Direction) {
	case "inbound":
		return e.Caller.PhoneNumber, e.Caller.Name
	case "outbound":
		return e.Callee.PhoneNumber, e.Callee.Name
	}
	// 最終フォールバック: callee を客とする (発信が業務の中心ユースケース)
	return e.Callee.PhoneNumber, e.Callee.Name
}

// parseZoomTime は Zoom 系 time フィールドの優先順序で最初に parse 成功した
// 値を返す。全部失敗時は time.Now()。
func parseZoomTime(candidates ...string) time.Time {
	for _, s := range candidates {
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02T15:04:05Z", s); err == nil {
			return t
		}
	}
	return time.Now()
}
