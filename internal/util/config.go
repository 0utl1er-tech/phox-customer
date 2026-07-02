package util

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

// Config stores all configuration of the application.
// The values are read by viper from a config file or environment variable.
type Config struct {
	Environment          string `mapstructure:"ENV"`
	DBSource             string `mapstructure:"DB_SOURCE"`
	ConnectServerAddress string `mapstructure:"CONNECT_SERVER_ADDRESS"`

	// JWT verification settings (generic OIDC — currently wired to Keycloak).
	// JWTProjectID is the expected OAuth2 audience of incoming access tokens.
	// For Keycloak, this is set via an oidc-audience-mapper on the public client.
	JWTEnabled   bool   `mapstructure:"JWT_ENABLED"`
	JWTProjectID string `mapstructure:"JWT_PROJECT_ID"`
	JWTIssuerURL string `mapstructure:"JWT_ISSUER_URL"`
	JWTJwksURL   string `mapstructure:"JWT_JWKS_URL"`

	// Keycloak Admin API settings — used by CreateCompanyUser to provision users.
	KeycloakURL               string `mapstructure:"KEYCLOAK_URL"`
	KeycloakRealm             string `mapstructure:"KEYCLOAK_REALM"`
	KeycloakAdminClientID     string `mapstructure:"KEYCLOAK_ADMIN_CLIENT_ID"`
	KeycloakAdminClientSecret string `mapstructure:"KEYCLOAK_ADMIN_CLIENT_SECRET"`

	// Elasticsearch URL — used by SearchService and the customer indexer.
	// If empty, search features are disabled (degraded mode; indexing is a no-op).
	ElasticsearchURL string `mapstructure:"ELASTICSEARCH_URL"`

	// SMTP outbound mail (for CreateActivityEmailSent).
	// SMTPTLSMode は "none" / "implicit" / "starttls" の 3 値:
	//   - implicit: port 465 SMTPS (mailu 本番で推奨、STARTTLS 非サポート)
	//   - none:     MailHog 1025 など平文
	//   - starttls: port 587 STARTTLS (mailu 以外のサーバー向け)
	SMTPHost        string `mapstructure:"SMTP_HOST"`
	SMTPPort        int    `mapstructure:"SMTP_PORT"`
	SMTPUsername    string `mapstructure:"SMTP_USERNAME"`
	SMTPPassword    string `mapstructure:"SMTP_PASSWORD"`
	SMTPDefaultFrom string `mapstructure:"SMTP_DEFAULT_FROM"`
	SMTPTLSMode     string `mapstructure:"SMTP_TLS_MODE"`

	// IMAP inbound mail for history ingestion (Phase 14).
	// 空なら IMAP worker は起動しない (dev/MailHog 環境の default)。
	// Phase 14b: IMAP worker 本実装に合わせて TLS mode / Sent / INBOX を分離。
	IMAPHost                  string `mapstructure:"IMAP_HOST"`
	IMAPPort                  int    `mapstructure:"IMAP_PORT"`
	IMAPTLSMode               string `mapstructure:"IMAP_TLS_MODE"` // "implicit"|"starttls"|"none"
	IMAPTLSInsecureSkipVerify bool   `mapstructure:"IMAP_TLS_INSECURE_SKIP_VERIFY"`
	IMAPUsername              string `mapstructure:"IMAP_USERNAME"`
	IMAPPassword              string `mapstructure:"IMAP_PASSWORD"`
	IMAPSentMailbox           string `mapstructure:"IMAP_SENT_MAILBOX"`
	IMAPInboxMailbox          string `mapstructure:"IMAP_INBOX_MAILBOX"`
	IMAPPollInterval          string `mapstructure:"IMAP_POLL_INTERVAL"`
	// "system" の User.id (000003_create_activity で seed 済)。取込んだ activity
	// の user_id に使う。override 可だが基本触らない。
	IMAPIngestUserID string `mapstructure:"IMAP_INGEST_USER_ID"`

	// Phase 20: Google Calendar 連携 (Redial)。
	// GCalMode: "real" = 実 GCal API, "mock" = dev/E2E 用フェイク (debug endpoint 有効)
	// GCalTokenKey: AES-GCM 暗号化鍵 (base64 32 byte)。mock mode でも必須 (OAuth フローで使う)
	GCalMode            string `mapstructure:"GCAL_MODE"`
	GCalTokenKey        string `mapstructure:"GCAL_TOKEN_KEY"`
	GoogleOAuthClientID string `mapstructure:"GOOGLE_OAUTH_CLIENT_ID"`
	GoogleOAuthSecret   string `mapstructure:"GOOGLE_OAUTH_CLIENT_SECRET"`
	GoogleOAuthRedirect string `mapstructure:"GOOGLE_OAUTH_REDIRECT_URL"`
	// Phox の顧客詳細 URL のベース (GCal イベントの description 内に貼る)。
	// 例: http://localhost:3000
	PhoxBaseURL string `mapstructure:"PHOX_BASE_URL"`

	// Phase 21: Zoom Phone API (Server-to-Server OAuth)
	ZoomAccountID    string `mapstructure:"ZOOM_ACCOUNT_ID"`
	ZoomClientID     string `mapstructure:"ZOOM_CLIENT_ID"`
	ZoomClientSecret string `mapstructure:"ZOOM_CLIENT_SECRET"`
	// Zoom App の Secret Token (webhook signature verification + URL validation HMAC)。
	// 空なら署名検証をスキップ (dev / 移行期 only)。本番では必ず設定すること。
	ZoomWebhookSecret string `mapstructure:"ZOOM_WEBHOOK_SECRET"`

	// Phase 22: 通話録音アーカイブ用 S3 (Ceph RGW). OBC `phox-recordings-s3` 由来。
	// 全空なら recording_archiver は disabled (= recording_url 未保存)。
	// PHOX_RECORDING_S3_USE_TLS は cluster 内 Ceph RGW なら false (HTTP)。
	RecordingS3Endpoint  string `mapstructure:"PHOX_RECORDING_S3_ENDPOINT"`
	RecordingS3AccessKey string `mapstructure:"PHOX_RECORDING_S3_ACCESS_KEY"`
	RecordingS3SecretKey string `mapstructure:"PHOX_RECORDING_S3_SECRET_KEY"`
	RecordingS3Bucket    string `mapstructure:"PHOX_RECORDING_S3_BUCKET"`
	RecordingS3Region    string `mapstructure:"PHOX_RECORDING_S3_REGION"`
	RecordingS3UseTLS    bool   `mapstructure:"PHOX_RECORDING_S3_USE_TLS"`

	// Phase 22b: 録音再生用の short-lived signed URL の HMAC 鍵。
	// UI は GetActivityRecording RPC で発行された URL を <audio src=...> に
	// 渡すだけで良い (Bearer token を URL に乗せる必要がない)。鍵は base64 で
	// 32 byte 以上推奨。空なら GetActivityRecording は CodeUnavailable を返す。
	RecordingURLSigningKey string `mapstructure:"PHOX_RECORDING_URL_SIGNING_KEY"`
	// signed URL の publish base (browser から到達する公開 URL)。
	// 例: https://phox-api.0utl1er.tech / dev: http://localhost:8082
	// 末尾スラッシュ付けない。空なら ICAL_FEED_BASE_URL にフォールバック。
	APIPublicBaseURL string `mapstructure:"PHOX_API_PUBLIC_BASE_URL"`

	// Phase 23: SSE 配信を pod 跨ぎで届けるための Redis pub/sub backend。
	// 単 pod 運用ではこの env を空のままにすれば in-memory hub に fallback する。
	// host:port 形式 (auth は cluster 内なので省略)。
	RedisAddr string `mapstructure:"REDIS_ADDR"`

	// Phase 20e: iCalendar 購読 URL のベース。phox-customer 自身が listen する
	// 外部到達 URL (ブラウザやカレンダークライアントから見える URL)。
	// 例 dev: http://localhost:8082  /  prod: https://phox-api.example.com
	// 未設定時は main.go で `http://{CONNECT_SERVER_ADDRESS}` にフォールバック。
	ICalFeedBaseURL string `mapstructure:"ICAL_FEED_BASE_URL"`

	// Phase 24: MCP (Model Context Protocol) server — Streamable HTTP を
	// /mcp にマウントする。認証は Connect RPC と同一の Keycloak JWT。
	// false でエンドポイントごと無効化。
	MCPEnabled bool `mapstructure:"MCP_ENABLED"`
}

// LoadConfig reads configuration from `app.env` and (if present) layers
// `app.env.local` on top. `app.env.local` is .gitignored and is meant for
// developer-specific secrets (e.g. real Google OAuth credentials) that should
// not be committed.
//
// 優先順位 (高いほど勝つ):
//  1. 実行時環境変数 (viper.AutomaticEnv)
//  2. app.env.local   (developer override, gitignored)
//  3. app.env         (committed defaults)
func LoadConfig(path string) (config Config, err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("app")
	viper.SetConfigType("env")

	// Enable automatic environment variable binding
	viper.AutomaticEnv()

	// Layer 1: base defaults from app.env
	if err = viper.ReadInConfig(); err != nil {
		return
	}

	// Layer 2: developer overrides from app.env.local (optional, gitignored)
	// SetConfigFile で絶対パス or 相対パスを直接指定できる (ConfigName + Type では
	// app.env.local.env を探してしまうため使えない)。
	localPath := filepath.Join(path, "app.env.local")
	if _, statErr := os.Stat(localPath); statErr == nil {
		viper.SetConfigFile(localPath)
		viper.SetConfigType("env")
		if merr := viper.MergeInConfig(); merr != nil {
			log.Warn().Err(merr).Str("path", localPath).Msg("Failed to merge app.env.local")
		} else {
			log.Info().Str("path", localPath).Msg("Loaded overrides from app.env.local")
		}
	}

	err = viper.Unmarshal(&config)

	// Debug log to verify config values
	log.Debug().
		Str("jwks_url", config.JWTJwksURL).
		Str("issuer_url", config.JWTIssuerURL).
		Str("audience", config.JWTProjectID).
		Bool("jwt_enabled", config.JWTEnabled).
		Str("keycloak_url", config.KeycloakURL).
		Str("keycloak_realm", config.KeycloakRealm).
		Str("gcal_mode", config.GCalMode).
		Msg("Config loaded")

	return
}
