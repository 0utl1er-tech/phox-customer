package main

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	activityv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1/activityv1connect"
	"github.com/0utl1er-tech/phox-customer/gen/pb/book/v1/bookv1connect"
	contactv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1/contactv1connect"
	customerv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1/customerv1connect"
	googleoauthv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/googleoauth/v1/googleoauthv1connect"
	icalfeedv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/icalfeed/v1/icalfeedv1connect"
	mailboxv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1/mailboxv1connect"
	mailtemplatev1connect "github.com/0utl1er-tech/phox-customer/gen/pb/mailtemplate/v1/mailtemplatev1connect"
	permitv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1/permitv1connect"
	redialv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/redial/v1/redialv1connect"
	"github.com/0utl1er-tech/phox-customer/gen/pb/search/v1/searchv1connect"
	statusv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/status/v1/statusv1connect"
	userv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1/userv1connect"
	zoomphonev1connect "github.com/0utl1er-tech/phox-customer/gen/pb/zoomphone/v1/zoomphonev1connect"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	"github.com/0utl1er-tech/phox-customer/internal/gcal"
	"github.com/0utl1er-tech/phox-customer/internal/ical"
	"github.com/0utl1er-tech/phox-customer/internal/keycloakadmin"
	"github.com/0utl1er-tech/phox-customer/internal/mail"
	"github.com/0utl1er-tech/phox-customer/internal/mailu"
	"github.com/0utl1er-tech/phox-customer/internal/mcpserver"
	oauthsvc "github.com/0utl1er-tech/phox-customer/internal/oauth"
	"github.com/0utl1er-tech/phox-customer/internal/recording"
	"github.com/0utl1er-tech/phox-customer/internal/schemaguard"
	"github.com/0utl1er-tech/phox-customer/internal/search"
	"github.com/0utl1er-tech/phox-customer/internal/service/activity"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/0utl1er-tech/phox-customer/internal/service/book"
	"github.com/0utl1er-tech/phox-customer/internal/service/contact"
	"github.com/0utl1er-tech/phox-customer/internal/service/customer"
	"github.com/0utl1er-tech/phox-customer/internal/service/googleoauth"
	"github.com/0utl1er-tech/phox-customer/internal/service/icalfeed"
	"github.com/0utl1er-tech/phox-customer/internal/service/mailbox"
	"github.com/0utl1er-tech/phox-customer/internal/service/mailtemplate"
	"github.com/0utl1er-tech/phox-customer/internal/service/permit"
	"github.com/0utl1er-tech/phox-customer/internal/service/redial"
	searchsvc "github.com/0utl1er-tech/phox-customer/internal/service/search"
	"github.com/0utl1er-tech/phox-customer/internal/service/status"
	"github.com/0utl1er-tech/phox-customer/internal/service/user"
	zoomphoneservice "github.com/0utl1er-tech/phox-customer/internal/service/zoomphone"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/0utl1er-tech/phox-customer/internal/zoom"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/sync/errgroup"
	googlecalendar "google.golang.org/api/calendar/v3"
)

// migrationFS はスキーマ版ズレ検出 (schemaguard) 用に migration ファイル名を
// バイナリへ埋め込む。実行されるのはあくまで start.sh / CI の migrate CLI で、
// ここでは「バイナリが期待する最新 version」を知るためだけに使う。
//
//go:embed db/migration/*.up.sql
var migrationFS embed.FS

// safePrefix は ログ用に文字列の先頭 N 文字を返す。機密値を ***** で隠す。
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s + "..."
	}
	return s[:n] + "..."
}

// corsMiddleware adds CORS headers to allow cross-origin requests
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// すべてのオリジンからのリクエストを許可
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Connect-Protocol-Version, Connect-Timeout-Ms")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	cfg, err := util.LoadConfig(".")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	// CLI subcommand: `go run . reindex` — rebuilds the ES customer index
	// from the application DB. See reindex.go for the implementation.
	if len(os.Args) > 1 && os.Args[1] == "reindex" {
		runReindex(cfg)
		return
	}

	// CLI subcommand: `phox-customer backfill [--since 24h]` — fetch past
	// Zoom Phone call_logs + recordings via REST API and upsert Activity
	// rows. Idempotent (zoom_call_id UNIQUE), safe to re-run.
	if len(os.Args) > 1 && os.Args[1] == "backfill" {
		runBackfill(cfg, os.Args[2:])
		return
	}

	// CLI subcommand: `phox-customer reconcile-mailbox` — link already-ingested
	// MailboxMessages to matching customers (Activity + customer_id). Idempotent.
	// See reconcile_mailbox.go.
	if len(os.Args) > 1 && os.Args[1] == "reconcile-mailbox" {
		runReconcileMailbox(cfg)
		return
	}

	connPool, err := pgxpool.New(context.Background(), cfg.DBSource)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create connection pool")
	}
	defer connPool.Close()

	// Test database connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := connPool.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to ping database - connection not established")
	}

	log.Info().Msg("Database connection established successfully")

	// スキーマ版ズレの fail-fast。migrate を実行しない直起動 (go run . / air)
	// が古い DB に当たると初回 INSERT まで 42703 が遅延して出るため、
	// 起動時点で検出して修正コマンド付きで止める。
	if err := schemaguard.Verify(ctx, connPool, migrationFS, "db/migration"); err != nil {
		log.Fatal().Err(err).Msg("DB schema version check failed")
	}

	queries := db.New(connPool)

	// Initialize Keycloak Admin client — CreateCompanyUser depends on this,
	// so a misconfigured admin client is a fatal error (fail loud at boot
	// rather than at first call).
	keycloakAdmin, err := keycloakadmin.NewClient(context.Background(), cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Keycloak admin client")
	}
	log.Info().
		Str("keycloak_url", cfg.KeycloakURL).
		Str("realm", cfg.KeycloakRealm).
		Msg("Keycloak admin client initialized successfully")

	// Elasticsearch client (optional — degraded mode if ES is unreachable).
	esClient := newESClientOrWarn(cfg)
	indexer := search.NewIndexer(esClient)
	if indexer.Enabled() {
		log.Info().Str("url", cfg.ElasticsearchURL).Msg("Elasticsearch indexer initialized")
	}

	// SMTP mail client (optional — degraded mode if SMTP_HOST is empty).
	mailClient, err := mail.NewSMTPClient(mail.Config{
		Host:        cfg.SMTPHost,
		Port:        cfg.SMTPPort,
		Username:    cfg.SMTPUsername,
		Password:    cfg.SMTPPassword,
		DefaultFrom: cfg.SMTPDefaultFrom,
		TLSMode:     cfg.SMTPTLSMode,
	})
	if err != nil {
		log.Warn().Err(err).Msg("failed to initialize SMTP client — email send will be unavailable")
		mailClient = nil
	} else if mailClient != nil {
		log.Info().
			Str("host", cfg.SMTPHost).
			Int("port", cfg.SMTPPort).
			Str("tls_mode", cfg.SMTPTLSMode).
			Msg("SMTP client initialized")
	} else {
		log.Warn().Msg("SMTP_HOST not set — email send will be unavailable")
	}

	// IMAP worker (Phase 14b 本実装)。IMAP_HOST が空なら起動せず no-op。
	// dev default は MailHog で IMAP 無し → Enabled() == false。
	// app.env.local で mailu の SMTPS/IMAPS に向けると polling loop が走る。
	imapWorker := mail.NewIMAPWorker(mail.IMAPWorkerConfig{
		Host:                  cfg.IMAPHost,
		Port:                  cfg.IMAPPort,
		TLSMode:               mail.IMAPTLSMode(cfg.IMAPTLSMode),
		TLSInsecureSkipVerify: cfg.IMAPTLSInsecureSkipVerify,
		Username:              cfg.IMAPUsername,
		Password:              cfg.IMAPPassword,
		SentMailbox:           cfg.IMAPSentMailbox,
		InboxMailbox:          cfg.IMAPInboxMailbox,
		PollInterval:          cfg.IMAPPollInterval,
		IngestUserID:          cfg.IMAPIngestUserID,
	}, queries)

	// JIT user provisioning needs a Company UUID to stamp on auto-created
	// User rows. Single-tenant deployments only have one row — take the
	// first. If someone runs multi-tenant in the future this becomes a
	// per-token decision (e.g., derived from a group / claim mapper).
	companies, err := queries.ListCompanies(context.Background())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to list companies for JIT provisioning bootstrap")
	}
	if len(companies) == 0 {
		log.Fatal().Msg("No Company row present — cannot derive default company for JIT provisioning. Seed Company first.")
	}
	defaultCompanyID := companies[0].ID
	log.Info().
		Str("company_id", defaultCompanyID.String()).
		Str("company_name", companies[0].Name).
		Msg("Default company resolved for JIT provisioning")

	// Create interceptor
	authInterceptor := auth.NewAuthInterceptor(context.Background(), queries, cfg, defaultCompanyID)
	interceptors := connect.WithInterceptors(authInterceptor)

	// Create services
	customerService := customer.NewCustomerService(queries, indexer)
	bookService := book.NewBookService(queries, indexer)
	permitService := permit.NewPermitService(queries)
	contactService := contact.NewContactService(queries)
	statusService := status.NewStatusService(queries)

	// Phase 25: MailboxService — MAILBOX_SECRET_KEY 設定時のみ有効。
	// 鍵はメールボックスパスワードの AES-GCM 暗号化に使う。cipher と共有 mailu
	// SMTP 接続 (MailboxSender) は後段で activityService の実メールボックス
	// 送信にも注入する。
	var mailboxService *mailbox.MailboxService
	var mailboxCipher *crypto.Cipher
	var mailboxSender *mail.MailboxSender
	if cfg.MailboxSecretKey != "" {
		var cerr error
		mailboxCipher, cerr = crypto.NewCipherFromBase64(cfg.MailboxSecretKey)
		if cerr != nil {
			log.Fatal().Err(cerr).Msg("Failed to decode MAILBOX_SECRET_KEY")
		}
		// Phase 25/D: mailu 管理 API クライアント (両 env 揃うと自動作成有効)。
		mailuClient := mailu.NewClient(cfg.MailuAPIBase, cfg.MailuAPIToken)
		mailboxService = mailbox.NewMailboxService(queries, mailboxCipher, mailuClient)
		mailboxSender, cerr = mail.NewMailboxSender(cfg.MailuSMTPHost, cfg.MailuSMTPPort, cfg.MailuSMTPTLS)
		if cerr != nil {
			log.Fatal().Err(cerr).Msg("Failed to build mailbox sender")
		}
		log.Info().
			Str("mailu_smtp", cfg.MailuSMTPHost).
			Bool("sending_enabled", mailboxSender != nil).
			Bool("auto_provision", mailuClient != nil).
			Msg("MailboxService enabled")
	} else {
		log.Warn().Msg("MAILBOX_SECRET_KEY not set — MailboxService disabled")
	}

	// Phase 25/C: DB 駆動のマルチメールボックス IMAP worker。
	// MAILU_IMAP_HOST + MAILBOX_SECRET_KEY (mailboxCipher) が揃うと有効。
	// DB の active な Mailbox を全て polling し、Activity.mailbox_id を記録する。
	mailboxIMAPWorker := mail.NewMailboxIMAPWorker(
		mail.IMAPConnBase{
			Host:                  cfg.MailuIMAPHost,
			Port:                  cfg.MailuIMAPPort,
			TLSMode:               mail.IMAPTLSMode(cfg.MailuIMAPTLS),
			TLSInsecureSkipVerify: cfg.IMAPTLSInsecureSkipVerify,
		},
		cfg.IMAPSentMailbox, cfg.IMAPInboxMailbox, cfg.IMAPPollInterval, cfg.IMAPIngestUserID,
		queries, mailboxCipher,
	)

	userService := user.NewUserService(queries, keycloakAdmin, connPool)
	searchService := searchsvc.NewSearchService(queries, esClient)
	// Activity service — Phase 11 で SMTPClient を注入済み。
	// SMTP が未設定の場合 mailClient は nil で、CreateActivityEmailSent は
	// Unavailable エラーを返す (degraded mode)。
	// activityService は Phase 22 archiver 初期化後に組み立てる (recording.Service
	// を注入するため)。下の Phase 22 ブロックを参照。
	mailTemplateService := mailtemplate.NewMailTemplateService(queries)

	// Phase 20: Google Calendar 連携
	// AES-GCM 鍵は必須 (mock mode でも OAuth state 署名に使うため)。
	if cfg.GCalTokenKey == "" {
		log.Fatal().Msg("GCAL_TOKEN_KEY is required (base64-encoded 32 byte key)")
	}
	gcalCipher, err := crypto.NewCipherFromBase64(cfg.GCalTokenKey)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to decode GCAL_TOKEN_KEY")
	}

	var gcalClient gcal.Client
	var gcalMock *gcal.MockClient
	switch cfg.GCalMode {
	case "", "real":
		// real client — config 検証 (dummy キーは fail fast)
		if cfg.GoogleOAuthClientID == "" || strings.HasPrefix(cfg.GoogleOAuthClientID, "dummy") {
			log.Fatal().
				Str("GOOGLE_OAUTH_CLIENT_ID", cfg.GoogleOAuthClientID).
				Msg("GCAL_MODE=real requires a real GOOGLE_OAUTH_CLIENT_ID. " +
					"Create one at https://console.cloud.google.com/apis/credentials or set GCAL_MODE=mock")
		}
		if cfg.GoogleOAuthSecret == "" || strings.HasPrefix(cfg.GoogleOAuthSecret, "dummy") {
			log.Fatal().Msg("GCAL_MODE=real requires a real GOOGLE_OAUTH_CLIENT_SECRET")
		}
		if cfg.GoogleOAuthRedirect == "" {
			log.Fatal().Msg("GCAL_MODE=real requires GOOGLE_OAUTH_REDIRECT_URL")
		}
		// real client — oauth2.Config を組み立て (Google 公式 endpoint + scope)
		oauthCfg := &oauth2.Config{
			ClientID:     cfg.GoogleOAuthClientID,
			ClientSecret: cfg.GoogleOAuthSecret,
			RedirectURL:  cfg.GoogleOAuthRedirect,
			Scopes: []string{
				googlecalendar.CalendarEventsScope,
				"https://www.googleapis.com/auth/userinfo.email",
			},
			Endpoint: google.Endpoint,
		}
		gcalClient = gcal.NewRealClient(oauthCfg, gcalCipher, queries)
		log.Info().
			Str("client_id_prefix", safePrefix(cfg.GoogleOAuthClientID, 12)).
			Str("redirect", cfg.GoogleOAuthRedirect).
			Msg("GCal client: real")
	case "mock":
		gcalMock = gcal.NewMockClient()
		gcalClient = gcalMock
		log.Info().Msg("GCal client: mock (E2E/dev)")
	default:
		log.Fatal().Str("mode", cfg.GCalMode).Msg("unknown GCAL_MODE")
	}

	redialService := redial.NewRedialService(queries, gcalClient)
	googleOAuthService := googleoauth.NewGoogleOAuthService(queries)

	oauthHandler, err := oauthsvc.NewHandler(cfg, gcalCipher, queries)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to init oauth handler")
	}

	// Phase 20e: iCalendar 購読フィード
	icalFeedBaseURL := cfg.ICalFeedBaseURL
	if icalFeedBaseURL == "" {
		icalFeedBaseURL = "http://" + cfg.ConnectServerAddress
	}
	icalFeedService := icalfeed.NewICalFeedService(queries, icalFeedBaseURL)
	icalHandler := ical.NewHandler(queries, cfg.PhoxBaseURL)

	// Phase 21: Zoom Phone API
	zoomClient := zoom.NewClient(zoom.Config{
		AccountID:    cfg.ZoomAccountID,
		ClientID:     cfg.ZoomClientID,
		ClientSecret: cfg.ZoomClientSecret,
	})
	if zoomClient != nil {
		log.Info().Msg("Zoom Phone client initialized")
	} else {
		log.Warn().Msg("Zoom Phone not configured (ZOOM_ACCOUNT_ID/CLIENT_ID/CLIENT_SECRET missing)")
	}
	zoomPhoneService := zoomphoneservice.NewZoomPhoneService(queries, zoomClient)

	// Phase 22: Recording archiver (Zoom 録音 → Ceph RGW 永続化)
	// OBC `phox-recordings-s3` 由来の env vars。空なら archiver disabled。
	recordingArchiver, recArchErr := zoom.NewRecordingArchiver(
		zoomClient,
		cfg.RecordingS3Endpoint,
		cfg.RecordingS3AccessKey,
		cfg.RecordingS3SecretKey,
		cfg.RecordingS3Bucket,
		cfg.RecordingS3Region,
		cfg.RecordingS3UseTLS,
	)
	if recArchErr != nil {
		log.Warn().Err(recArchErr).Msg("Recording archiver init failed — recordings will not be persisted")
	} else if recordingArchiver != nil {
		log.Info().
			Str("endpoint", cfg.RecordingS3Endpoint).
			Str("bucket", cfg.RecordingS3Bucket).
			Msg("Recording archiver initialized")
	} else {
		log.Info().Msg("Recording archiver disabled (S3 config missing)")
	}

	// Phase 22b: 録音再生用 signed URL の発行 + ストリーミングハンドラ。
	// archiver と同じ minio client を再利用する (Bucket / 認証情報は同一)。
	// Signing key 不在 / archiver 不在のいずれかで disabled に丸められる。
	publicBaseURL := cfg.APIPublicBaseURL
	if publicBaseURL == "" {
		publicBaseURL = cfg.ICalFeedBaseURL // 同じ host が listen してるので fallback
	}
	var s3Client *minio.Client
	if recordingArchiver != nil {
		s3Client = recordingArchiver.S3()
	}
	recordingSvc := recording.NewService(
		queries,
		auth.NewAuthorizer(queries),
		s3Client,
		cfg.RecordingURLSigningKey,
		publicBaseURL,
	)
	if recordingSvc.IsEnabled() {
		log.Info().Str("public_base", publicBaseURL).Msg("Recording playback (signed URL) enabled")
	} else {
		log.Info().Msg("Recording playback disabled (signing key or S3 missing)")
	}

	// Activity service — recording.Service を注入してから construct。
	activityService := activity.NewActivityService(queries, mailClient, recordingSvc)
	// Phase 25: 実メールボックス送信 (mailbox_id 指定の CreateActivityEmailSent)。
	if mailboxSender != nil && mailboxCipher != nil {
		activityService = activityService.WithMailboxSending(mailboxSender, mailboxCipher)
		log.Info().Msg("Mailbox sending enabled for CreateActivityEmailSent")
	}

	// Phase 22: ActivityHandler — webhook event を Activity row に変換
	zoomActivityHandler := zoom.NewActivityHandler(queries, recordingArchiver, "system")

	// staff (phox 自社線) の Zoom Phone 番号を起動時にキャッシュ。
	// Zoom 側でユーザー追加 / 番号変更があった場合は phox-customer 再起動で再取得。
	if zoomClient != nil {
		if users, lerr := zoomClient.ListPhoneUsers(); lerr == nil {
			nums := make([]string, 0, len(users))
			for _, u := range users {
				if u.PhoneNumber != "" {
					nums = append(nums, u.PhoneNumber)
				}
			}
			zoomActivityHandler.SetStaffNumbers(nums)
		} else {
			log.Warn().Err(lerr).Msg("Zoom: ListPhoneUsers failed at startup — staff number cache empty, falling back to Direction field for caller/callee classification")
		}
	}

	// Zoom SSE hub (着信リアルタイム通知)。
	// REDIS_ADDR が設定されていれば pod 跨ぎ配信モード (Redis pub/sub)。
	// 空なら in-memory hub (単 pod / dev)。
	var sseHub *zoom.SSEHub
	var rdb *redis.Client
	if cfg.RedisAddr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
		// 起動時に PING で接続確認 — Redis 不通なら早めに失敗させる。
		if err := rdb.Ping(context.Background()).Err(); err != nil {
			log.Fatal().Err(err).Str("redis_addr", cfg.RedisAddr).Msg("Redis unreachable")
		}
		log.Info().Str("redis_addr", cfg.RedisAddr).Msg("Redis connected (SSE pub/sub backend)")
		sseHub = zoom.NewRedisSSEHub(rdb)
	} else {
		log.Info().Msg("REDIS_ADDR empty — using in-memory SSE hub (single-pod mode)")
		sseHub = zoom.NewSSEHub()
	}

	// Zoom Webhook handler (着信・通話終了・録音完了)
	// ZOOM_WEBHOOK_SECRET 設定時は signature 検証 + URL validation challenge を
	// その鍵で HMAC-SHA256 する。空なら署名検証スキップ (dev / 移行期)。
	zoomWebhook := zoom.NewWebhookHandler(cfg.ZoomWebhookSecret)
	if cfg.ZoomWebhookSecret != "" {
		log.Info().Msg("Zoom webhook signature verification enabled")
	} else {
		log.Warn().Msg("Zoom webhook secret not set — signature verification disabled")
	}
	zoomWebhook.OnIncomingRinging(func(ev zoom.PhoneCallEvent) {
		// 着信番号で Customer 逆引き (caller/callee の客側を取る)
		// outbound でも UI 側に「発信中」通知として SSE で流したい。
		callerSide := ev.Caller.PhoneNumber
		callerName := ev.Caller.Name
		if ev.Direction == "outbound" {
			callerSide = ev.Callee.PhoneNumber
			callerName = ev.Callee.Name
		}
		callerNorm := zoom.NormalizeJapanesePhone(callerSide)
		n := zoom.CallNotification{
			Type:         "ringing",
			CallID:       ev.CallID,
			CallerNumber: callerSide,
			CallerName:   callerName,
			Direction:    ev.Direction,
			Timestamp:    ev.RingingStartTime,
		}
		// Customer.phone / Contact.phone で逆引きを試行
		// (FindCustomerByEmail は mail 用なので、phone 逆引きは別途実装が望ましい。
		//  暫定: callerNorm で Customer.phone を LIKE 検索 — 将来クエリ追加)
		_ = callerNorm
		// 全ユーザーに broadcast (着信は誰が取るかわからない)
		sseHub.Broadcast("", n)
	})
	zoomWebhook.OnCallEnded(func(ev zoom.PhoneCallEvent) {
		// SSE broadcast (UI に通話終了通知) — caller を客側に揃える
		callerSide := ev.Caller.PhoneNumber
		if ev.Direction == "outbound" {
			callerSide = ev.Callee.PhoneNumber
		}
		sseHub.Broadcast("", zoom.CallNotification{
			Type:         "ended",
			CallID:       ev.CallID,
			CallerNumber: callerSide,
			Direction:    ev.Direction,
			Timestamp:    ev.CallEndTime,
		})
		// Activity row 作成 (Phase 22)
		zoomActivityHandler.HandleCallEnded(ev)
	})
	// Phase 22: 録音完了で Activity を更新
	zoomWebhook.OnRecordingComplete(zoomActivityHandler.HandleRecordingCompleted)

	// HTTPサーバーの設定
	mux := http.NewServeMux()

	// Connect-Goハンドラーを登録（ミドルウェア付き）
	customerPath, customerHandler := customerv1connect.NewCustomerServiceHandler(customerService, interceptors)
	bookPath, bookHandler := bookv1connect.NewBookServiceHandler(bookService, interceptors)
	permitPath, permitHandler := permitv1connect.NewPermitServiceHandler(permitService, interceptors)
	if mailboxService != nil {
		mailboxPath, mailboxHandler := mailboxv1connect.NewMailboxServiceHandler(mailboxService, interceptors)
		mux.Handle(mailboxPath, mailboxHandler)
	}
	contactPath, contactHandler := contactv1connect.NewContactServiceHandler(contactService, interceptors)
	statusPath, statusHandler := statusv1connect.NewStatusServiceHandler(statusService, interceptors)
	userPath, userHandler := userv1connect.NewUserServiceHandler(userService, interceptors)
	searchPath, searchHandler := searchv1connect.NewSearchServiceHandler(searchService, interceptors)
	activityPath, activityHandler := activityv1connect.NewActivityServiceHandler(activityService, interceptors)
	mailTemplatePath, mailTemplateHandler := mailtemplatev1connect.NewMailTemplateServiceHandler(mailTemplateService, interceptors)
	redialPath, redialHandler := redialv1connect.NewRedialServiceHandler(redialService, interceptors)
	googleOAuthPath, googleOAuthHandler := googleoauthv1connect.NewGoogleOAuthServiceHandler(googleOAuthService, interceptors)
	icalFeedPath, icalFeedHandler := icalfeedv1connect.NewICalFeedServiceHandler(icalFeedService, interceptors)
	zoomPhonePath, zoomPhoneHandler := zoomphonev1connect.NewZoomPhoneServiceHandler(zoomPhoneService, interceptors)

	mux.Handle(customerPath, customerHandler)
	mux.Handle(bookPath, bookHandler)
	mux.Handle(permitPath, permitHandler)
	mux.Handle(contactPath, contactHandler)
	mux.Handle(statusPath, statusHandler)
	mux.Handle(userPath, userHandler)
	mux.Handle(searchPath, searchHandler)
	mux.Handle(activityPath, activityHandler)
	mux.Handle(mailTemplatePath, mailTemplateHandler)
	mux.Handle(redialPath, redialHandler)
	mux.Handle(googleOAuthPath, googleOAuthHandler)
	mux.Handle(icalFeedPath, icalFeedHandler)
	mux.Handle(zoomPhonePath, zoomPhoneHandler)

	// OAuth HTTP endpoints (not Connect RPC — plain HTTP for browser redirects)
	mux.HandleFunc("/oauth/google/start", oauthHandler.StartHandler)
	mux.HandleFunc("/oauth/google/callback", oauthHandler.CallbackHandler)

	// Phase 20e: iCalendar feed endpoint
	mux.HandleFunc("GET /ical/{filename}", icalHandler.Serve)

	// Phase 21: Zoom Webhook + SSE
	mux.Handle("/webhook/zoom", zoomWebhook)
	mux.Handle("/sse/calls", sseHub)

	// Phase 22b: 録音再生 streaming endpoint。
	// signed URL は GetActivityRecording RPC で発行され、UI が <audio src="">
	// に直接渡す形で再生する。disabled でも mux 登録はしておく (handler 側で 503)。
	mux.Handle("GET /recordings/{activity_id}", recordingSvc)

	// Phase 24: MCP server (Streamable HTTP)。認証は Connect RPC と同一
	// (authInterceptor.Authenticate) — ツールは既存 service を in-process で
	// 呼ぶので Permit / role チェックもそのまま適用される。
	if cfg.MCPEnabled {
		// OAuth discovery (RFC 9728): 公開 URL が分かる場合のみ有効化。
		// resource_metadata 付きの 401 → クライアントが Keycloak を発見して
		// 認可コードフロー + 自動リフレッシュに乗る。
		metaURL := ""
		apiBase := strings.TrimSuffix(cfg.APIPublicBaseURL, "/")
		if apiBase != "" {
			metaURL = apiBase + "/.well-known/oauth-protected-resource/mcp"
			metaHandler := mcpserver.ProtectedResourceMetadataHandler(apiBase+"/mcp", cfg.JWTIssuerURL)
			mux.Handle("/.well-known/oauth-protected-resource", metaHandler)
			mux.Handle("/.well-known/oauth-protected-resource/mcp", metaHandler)
		}
		mux.Handle("/mcp", mcpserver.NewHandler(authInterceptor, mcpserver.Deps{
			Book:     bookService,
			Customer: customerService,
			Contact:  contactService,
			Search:   searchService,
			Activity: activityService,
			Mailbox:  mailboxService, // nil 可 (機能無効時は list_mailboxes 非登録)
			Queries:  queries,        // create_customer の upsert 判定用
		}, metaURL))
		log.Info().Str("resource_metadata", metaURL).Msg("MCP server mounted at /mcp")
	}

	// Debug endpoint — GCal mock 呼び出し履歴を返す (GCAL_MODE=mock のみ)
	if gcalMock != nil {
		mux.HandleFunc("/debug/gcal/calls", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(gcalMock.Calls())
		})
		mux.HandleFunc("/debug/gcal/reset", func(w http.ResponseWriter, r *http.Request) {
			gcalMock.Reset()
			w.WriteHeader(http.StatusNoContent)
		})
	}

	// CORSミドルウェアを適用
	corsHandler := corsMiddleware(mux)

	// HTTP/2対応のサーバーを作成。
	// MaxConcurrentStreams: Cilium Gateway (Envoy) は複数リクエストを 1 本の
	// h2c コネクションに多重化する。長命な SSE (/sse/calls) がストリームを
	// 占有するため、既定 250 だと SSE が溜まった時に同じコネクション上の
	// 通常 RPC が枯渇してブロックし得る。SSE 側の heartbeat/寿命でリークは
	// 塞いだ (internal/zoom/sse.go) が、多重化の頭打ちを避けるため上限も上げる。
	h2s := &http2.Server{MaxConcurrentStreams: 2000}
	server := &http.Server{
		Addr:    cfg.ConnectServerAddress,
		Handler: h2c.NewHandler(corsHandler, h2s),
	}

	// サーバー起動とGraceful Shutdown
	waitGroup, ctx := errgroup.WithContext(context.Background())

	// IMAP worker (Phase 14 stub) も errgroup に載せる — IMAP_HOST 空なら即 return。
	if imapWorker.Enabled() {
		waitGroup.Go(func() error {
			return imapWorker.Run(ctx)
		})
	}

	// Phase 25/C: DB 駆動のマルチメールボックス IMAP worker。
	if mailboxIMAPWorker != nil {
		waitGroup.Go(func() error {
			return mailboxIMAPWorker.Run(ctx)
		})
	}

	// SSE Redis subscriber (REDIS_ADDR 未設定時は即 nil 返却で無害)。
	waitGroup.Go(func() error {
		return sseHub.Run(ctx)
	})

	waitGroup.Go(func() error {
		log.Info().Msgf("Start Connect-Go server at %s", server.Addr)
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Connect-Go server failed to serve")
			return err
		}
		return nil
	})

	waitGroup.Go(func() error {
		<-ctx.Done()
		log.Info().Msg("Graceful shutdown Connect-Go server")
		err := server.Shutdown(context.Background())
		if err != nil {
			log.Error().Err(err).Msg("Failed to shutdown server gracefully")
			return err
		}
		log.Info().Msg("Connect-Go server is stopped")
		return nil
	})

	err = waitGroup.Wait()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to wait")
	}
}
