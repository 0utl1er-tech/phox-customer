package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/0utl1er-tech/phox-customer/internal/zoom"
)

// runBackfill は `phox-customer backfill --since 168h` の entry point。
// Zoom Phone REST API から call_logs / recordings を取得し、
// Activity に upsert + 録音を Ceph RGW に archive する。
//
// 使い方:
//
//	# デフォルト (直近 24 時間)
//	phox-customer backfill
//
//	# 過去 7 日
//	phox-customer backfill --since 168h
//
//	# 明示的な範囲
//	phox-customer backfill --from 2026-04-01 --to 2026-04-30
//
// Zoom API の制約で 1 リクエストの from/to 範囲は最大 1 ヶ月。長期間を
// 指定した場合は内部的に月単位に分割して連続呼び出しする。
func runBackfill(cfg util.Config, args []string) {
	fs := flag.NewFlagSet("backfill", flag.ExitOnError)
	since := fs.Duration("since", 24*time.Hour, "look back duration (e.g. 168h for 7 days). Ignored if --from/--to specified.")
	fromStr := fs.String("from", "", "start date YYYY-MM-DD (inclusive). When set, --to is required.")
	toStr := fs.String("to", "", "end date YYYY-MM-DD (inclusive). When set, --from is required.")
	if err := fs.Parse(args); err != nil {
		log.Fatal().Err(err).Msg("backfill: flag parse")
	}

	// 期間の決定
	var from, to time.Time
	switch {
	case *fromStr != "" && *toStr != "":
		var err error
		from, err = time.Parse("2006-01-02", *fromStr)
		if err != nil {
			log.Fatal().Err(err).Msg("backfill: bad --from")
		}
		to, err = time.Parse("2006-01-02", *toStr)
		if err != nil {
			log.Fatal().Err(err).Msg("backfill: bad --to")
		}
	case *fromStr == "" && *toStr == "":
		to = time.Now().UTC()
		from = to.Add(-*since)
	default:
		log.Fatal().Msg("backfill: --from and --to must be set together")
	}
	if to.Before(from) {
		log.Fatal().Msg("backfill: --to is before --from")
	}

	log.Info().
		Time("from", from).
		Time("to", to).
		Msg("backfill: starting")

	// === 依存組み立て ===
	connPool, err := pgxpool.New(context.Background(), cfg.DBSource)
	if err != nil {
		log.Fatal().Err(err).Msg("backfill: create connection pool")
	}
	defer connPool.Close()
	queries := db.New(connPool)

	zoomCfg := zoom.Config{
		AccountID:    cfg.ZoomAccountID,
		ClientID:     cfg.ZoomClientID,
		ClientSecret: cfg.ZoomClientSecret,
	}
	if !zoomCfg.Enabled() {
		log.Fatal().Msg("backfill: ZOOM_ACCOUNT_ID / ZOOM_CLIENT_ID / ZOOM_CLIENT_SECRET 必須")
	}
	zoomClient := zoom.NewClient(zoomCfg)

	archiver, err := zoom.NewRecordingArchiver(
		zoomClient,
		cfg.RecordingS3Endpoint,
		cfg.RecordingS3AccessKey,
		cfg.RecordingS3SecretKey,
		cfg.RecordingS3Bucket,
		cfg.RecordingS3Region,
		cfg.RecordingS3UseTLS,
	)
	if err != nil {
		log.Warn().Err(err).Msg("backfill: archiver init failed (continuing without recording archive)")
	}

	bf := zoom.NewBackfiller(zoomClient, queries, archiver, "system")

	// staff 番号キャッシュ (pickCustomerSide 用)
	if users, lerr := zoomClient.ListPhoneUsers(); lerr == nil {
		nums := make([]string, 0, len(users))
		for _, u := range users {
			if u.PhoneNumber != "" {
				nums = append(nums, u.PhoneNumber)
			}
		}
		bf.SetStaffNumbers(nums)
		log.Info().Int("staff_phone_count", len(nums)).Msg("backfill: staff numbers cached")
	} else {
		log.Warn().Err(lerr).Msg("backfill: ListPhoneUsers failed — staff cache empty, falling back to direction")
	}

	// === 月単位に分割して実行 ===
	totalStats := zoom.BackfillStats{}
	for chunkStart := from; chunkStart.Before(to); {
		chunkEnd := chunkStart.AddDate(0, 1, 0)
		if chunkEnd.After(to) {
			chunkEnd = to
		}
		f := chunkStart.Format("2006-01-02")
		t := chunkEnd.Format("2006-01-02")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		stats, err := bf.Run(ctx, f, t)
		cancel()
		if err != nil {
			log.Error().Err(err).Str("from", f).Str("to", t).Msg("backfill: chunk failed")
			totalStats.Errors++
		} else {
			log.Info().
				Str("from", f).
				Str("to", t).
				Int("call_logs", stats.CallLogsFetched).
				Int("created", stats.ActivitiesCreated).
				Int("skipped", stats.ActivitiesSkipped).
				Int("recordings", stats.RecordingsFetched).
				Int("archived", stats.RecordingsArchived).
				Int("errors", stats.Errors).
				Msg("backfill: chunk done")
			totalStats.CallLogsFetched += stats.CallLogsFetched
			totalStats.ActivitiesCreated += stats.ActivitiesCreated
			totalStats.ActivitiesSkipped += stats.ActivitiesSkipped
			totalStats.RecordingsFetched += stats.RecordingsFetched
			totalStats.RecordingsArchived += stats.RecordingsArchived
			totalStats.Errors += stats.Errors
		}

		chunkStart = chunkEnd
	}

	log.Info().
		Int("total_call_logs", totalStats.CallLogsFetched).
		Int("total_created", totalStats.ActivitiesCreated).
		Int("total_skipped", totalStats.ActivitiesSkipped).
		Int("total_recordings", totalStats.RecordingsFetched).
		Int("total_archived", totalStats.RecordingsArchived).
		Int("total_errors", totalStats.Errors).
		Msg("backfill: complete")

	if totalStats.Errors > 0 {
		// CronJob 用に non-zero exit でアラート可能に。
		fmt.Fprintln(os.Stderr, "backfill: completed with errors")
		os.Exit(1)
	}
}
