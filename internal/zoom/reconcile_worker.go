package zoom

import (
	"context"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/rs/zerolog/log"
)

// Company.call_log_mode の値 (Phase 27f)。
const (
	// CallLogModeClick は従来モード — 電話番号クリック時にフォールバック
	// 経路でもコール活動を自動記録する (デフォルト)。
	CallLogModeClick = "click"
	// CallLogModeZoom は Zoom 通話履歴マスターモード — クリック時の
	// フォールバック自動記録を廃止し、この worker が毎時 call_logs を同期する。
	CallLogModeZoom = "zoom"
)

// reconcileInterval は定期リコンシリエーションの tick 間隔。
const reconcileInterval = time.Hour

// reconcileLookback は各 tick で同期する期間 (直近 24h)。webhook の
// 取りこぼしを回収するには十分に長く、Zoom API 負荷は軽い。
const reconcileLookback = 24 * time.Hour

// ReconcileWorker は Zoom call_logs の定期リコンシリエーション (Phase 27f)。
// Company.call_log_mode = 'zoom' のとき、直近 24h の call_logs / recordings を
// 毎時 Backfiller 経由で Activity に upsert し、webhook の取りこぼしを回収する。
//
// 取得 + upsert のロジックは backfill CLI (backfill_cmd.go) と同一の
// Backfiller を再利用する。
//
// 多重実行の安全性: Activity.zoom_call_id は UNIQUE で、Backfiller は
// zoom_call_id で dedup してから作成する冪等 upsert のため、replicas=2 で
// 両 pod が同時に tick しても二重記録にはならない (leader election 不要)。
type ReconcileWorker struct {
	client     *Client
	queries    *db.Queries
	backfiller *Backfiller
}

// NewReconcileWorker は worker を組み立てる。client が nil (ZOOM 未設定) でも
// worker 自体は生成し、tick ごとに skip ログを出す (設定漏れを観測可能にする)。
func NewReconcileWorker(client *Client, queries *db.Queries, archiver *RecordingArchiver, defaultUserID string) *ReconcileWorker {
	return &ReconcileWorker{
		client:     client,
		queries:    queries,
		backfiller: NewBackfiller(client, queries, archiver, defaultUserID),
	}
}

// Run は 1 時間毎の tick で reconcile を実行する (起動直後にも 1 回)。
// ctx cancel で正常終了する。エラーは tick 内で吸収し、worker は落とさない。
func (w *ReconcileWorker) Run(ctx context.Context) error {
	if w == nil {
		return nil
	}
	log.Info().Msg("zoom reconcile worker: started")

	// 起動直後に 1 回実行してから hourly tick。
	w.tick(ctx)

	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("zoom reconcile worker: stopped")
			return nil
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick は 1 回分の reconcile。gating (zoom client / call_log_mode) を通過
// したときだけ直近 24h の call_logs を同期する。
func (w *ReconcileWorker) tick(ctx context.Context) {
	// 会社の call_log_mode を取得。単一テナント前提: ListCompanies の先頭
	// (main.go の JIT provisioning bootstrap と同じ流儀)。マルチテナント化
	// する場合は会社ごとのループにする。
	mode := ""
	companies, err := w.queries.ListCompanies(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("zoom reconcile: list companies failed — skipping tick")
		return
	}
	if len(companies) > 0 {
		mode = companies[0].CallLogMode
	}

	if ok, reason := shouldReconcile(w.client != nil, mode); !ok {
		log.Info().Str("reason", reason).Msg("zoom reconcile: skipped")
		return
	}

	// staff 番号キャッシュを tick ごとに更新 (Zoom 側のユーザー変更に追従)。
	// 失敗しても前回キャッシュ (or 空 = direction fallback) で続行。
	if users, lerr := w.client.ListPhoneUsers(); lerr == nil {
		nums := make([]string, 0, len(users))
		for _, u := range users {
			if u.PhoneNumber != "" {
				nums = append(nums, u.PhoneNumber)
			}
		}
		w.backfiller.SetStaffNumbers(nums)
	} else {
		log.Warn().Err(lerr).Msg("zoom reconcile: ListPhoneUsers failed — using stale/empty staff cache")
	}

	from, to := reconcileWindow(time.Now().UTC(), reconcileLookback)
	log.Info().Str("from", from).Str("to", to).Msg("zoom reconcile: running")

	tickCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	stats, err := w.backfiller.Run(tickCtx, from, to)
	if err != nil {
		log.Error().Err(err).Msg("zoom reconcile: run failed")
		return
	}
	log.Info().
		Int("call_logs", stats.CallLogsFetched).
		Int("created", stats.ActivitiesCreated).
		Int("skipped", stats.ActivitiesSkipped).
		Int("recordings", stats.RecordingsFetched).
		Int("archived", stats.RecordingsArchived).
		Int("errors", stats.Errors).
		Msg("zoom reconcile: done")
}

// shouldReconcile は tick を実行すべきかの gating 判定 (純関数 — テスト対象)。
// 実行しない場合は skip 理由を返す。
func shouldReconcile(hasClient bool, callLogMode string) (bool, string) {
	if !hasClient {
		return false, "zoom client not configured"
	}
	if callLogMode != CallLogModeZoom {
		return false, "call_log_mode is not 'zoom' (mode=" + callLogMode + ")"
	}
	return true, ""
}

// reconcileWindow は now から lookback 遡った期間を Zoom API の
// YYYY-MM-DD (inclusive) 形式で返す (純関数 — テスト対象)。
// 日付単位に丸めるため、実際の取得範囲は lookback より広がる方向に倒れる
// (取りこぼし回収が目的なので広い分には冪等 upsert で無害)。
func reconcileWindow(now time.Time, lookback time.Duration) (from, to string) {
	return now.Add(-lookback).Format("2006-01-02"), now.Format("2006-01-02")
}
