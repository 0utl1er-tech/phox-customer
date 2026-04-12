//go:build gcal_integration
// +build gcal_integration

// Package gcal — 実 Google Calendar API に対する integration test。
//
// **CI では走らせない**。手元で一度連携を済ませた後の refresh_token を env で
// 渡して手動実行する想定:
//
//	# 1. /settings から Google 連携を済ませる
//	# 2. DB から UserGoogleToken を取り出す:
//	#    docker exec phox-manifest-postgres-1 psql -U root -d phox-customer \
//	#      -c "SELECT encode(refresh_token, 'base64') FROM \"UserGoogleToken\" LIMIT 1;"
//	#    ただし bytea は AES-GCM 暗号化されているので、そのままでは使えない。
//	#    もっと素直に: 最初の連携時に tok.RefreshToken を手元でメモしておくか、
//	#    本 test を下記形式で env から与える:
//	#
//	# 3. export:
//	#    GCAL_INTEGRATION_REFRESH_TOKEN=1//0g...   # Google から直接もらった値
//	#    GCAL_INTEGRATION_CLIENT_ID=xxx.apps.googleusercontent.com
//	#    GCAL_INTEGRATION_CLIENT_SECRET=GOCSPX-xxx
//	#
//	# 4. 実行:
//	#    go test -tags gcal_integration -v ./internal/gcal/...

package gcal

import (
	"context"
	"os"
	"testing"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"
	calendar "google.golang.org/api/calendar/v3"
)

const integrationUserID = "gcal-integration-test-user"

// inMemoryQueries は UserGoogleToken の読み書きだけ DB を使わず in-memory で
// 済ませる fake。integration test の焦点は実 Google API との疎通検証。
// Note: RealClient.tokenSource は db.Queries 型を直接要求するので、
// 本物の DB (phox-manifest-postgres-1) に直接繋ぐ方式を使う。
// test 用の User row + UserGoogleToken を手動で upsert する。

func getEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("integration test skipped: %s not set", key)
	}
	return v
}

func TestRealClient_CreateListPatchDelete(t *testing.T) {
	refreshToken := getEnv(t, "GCAL_INTEGRATION_REFRESH_TOKEN")
	clientID := getEnv(t, "GCAL_INTEGRATION_CLIENT_ID")
	clientSecret := getEnv(t, "GCAL_INTEGRATION_CLIENT_SECRET")

	ctx := context.Background()

	// 1. AES-GCM 鍵 (test 用固定値)
	key := []byte("0123456789abcdef0123456789abcdef") // 32 bytes
	cipher, err := crypto.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}

	// 2. oauth2.Config
	oauthCfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes: []string{
			calendar.CalendarEventsScope,
		},
		Endpoint: googleoauth.Endpoint,
	}

	// 3. DB に接続して test user + token を用意する。
	// ENV で DB_SOURCE を override 可能、無ければ local default。
	dbSource := os.Getenv("DB_SOURCE")
	if dbSource == "" {
		dbSource = "postgresql://root:secret@localhost:5432/phox-customer?sslmode=disable"
	}
	pool := mustPool(t, ctx, dbSource)
	defer pool.Close()
	q := db.New(pool)

	// テスト用 User 行を作成 (既存なら再利用)
	// system user を作成した同じ company_id を使う (mig 000003_create_activity で seed 済み)
	companyID := getTestCompanyID(t, ctx, pool)
	if _, err := q.GetUser(ctx, integrationUserID); err != nil {
		_, cerr := q.CreateUser(ctx, db.CreateUserParams{
			ID:        integrationUserID,
			CompanyID: companyID,
			Name:      "gcal integration test",
		})
		if cerr != nil {
			t.Fatalf("create test user: %v", cerr)
		}
	}

	// UserGoogleToken を upsert
	rtEnc, err := cipher.EncryptString(refreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.UpsertUserGoogleToken(ctx, db.UpsertUserGoogleTokenParams{
		UserID:       integrationUserID,
		RefreshToken: rtEnc,
		AccessToken:  nil,
		Expiry:       pgtype.Timestamptz{Valid: false},
		Scopes:       "https://www.googleapis.com/auth/calendar.events",
		GoogleEmail:  "integration@test",
	}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	t.Cleanup(func() {
		_ = q.DeleteUserGoogleToken(ctx, integrationUserID)
		// test user 行は他のテストから再利用したいので消さない
	})

	// 4. RealClient を組み立て、CRUD を実行
	client := NewRealClient(oauthCfg, cipher, q)

	start := time.Now().Add(24 * time.Hour).Truncate(time.Minute)
	end := start.Add(30 * time.Minute)

	t.Run("CreateEvent", func(t *testing.T) {
		eventID, err := client.CreateEvent(ctx, integrationUserID, EventInput{
			Summary:     "[Phox integration test] create",
			Description: "created by gcal_integration test",
			StartAt:     start,
			EndAt:       end,
			TimeZone:    "Asia/Tokyo",
		})
		if err != nil {
			t.Fatalf("CreateEvent: %v", err)
		}
		if eventID == "" {
			t.Fatal("empty event id")
		}
		t.Logf("created event id=%s", eventID)

		// PatchEvent
		patchedStart := start.Add(1 * time.Hour)
		patchedEnd := patchedStart.Add(45 * time.Minute)
		if err := client.PatchEvent(ctx, integrationUserID, eventID, EventInput{
			Summary:     "[Phox integration test] patched",
			Description: "patched by test",
			StartAt:     patchedStart,
			EndAt:       patchedEnd,
			TimeZone:    "Asia/Tokyo",
		}); err != nil {
			t.Fatalf("PatchEvent: %v", err)
		}

		// DeleteEvent
		if err := client.DeleteEvent(ctx, integrationUserID, eventID); err != nil {
			t.Fatalf("DeleteEvent: %v", err)
		}

		// DeleteEvent is idempotent — second call returns nil (404 swallowed)
		if err := client.DeleteEvent(ctx, integrationUserID, eventID); err != nil {
			t.Fatalf("DeleteEvent (second, should be idempotent): %v", err)
		}
	})
}

func TestRealClient_InvalidRefreshToken(t *testing.T) {
	clientID := getEnv(t, "GCAL_INTEGRATION_CLIENT_ID")
	clientSecret := getEnv(t, "GCAL_INTEGRATION_CLIENT_SECRET")

	ctx := context.Background()
	key := []byte("0123456789abcdef0123456789abcdef")
	cipher, err := crypto.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}

	oauthCfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{calendar.CalendarEventsScope},
		Endpoint:     googleoauth.Endpoint,
	}

	dbSource := os.Getenv("DB_SOURCE")
	if dbSource == "" {
		dbSource = "postgresql://root:secret@localhost:5432/phox-customer?sslmode=disable"
	}
	pool := mustPool(t, ctx, dbSource)
	defer pool.Close()
	q := db.New(pool)

	companyID := getTestCompanyID(t, ctx, pool)
	if _, err := q.GetUser(ctx, integrationUserID); err != nil {
		_, _ = q.CreateUser(ctx, db.CreateUserParams{
			ID:        integrationUserID,
			CompanyID: companyID,
			Name:      "gcal integration test",
		})
	}

	// 故意に壊れた refresh token を upsert
	rtEnc, _ := cipher.EncryptString("1//0g-clearly-invalid-refresh-token")
	_, _ = q.UpsertUserGoogleToken(ctx, db.UpsertUserGoogleTokenParams{
		UserID:       integrationUserID,
		RefreshToken: rtEnc,
		Scopes:       "https://www.googleapis.com/auth/calendar.events",
		GoogleEmail:  "integration@test",
	})
	t.Cleanup(func() {
		_ = q.DeleteUserGoogleToken(ctx, integrationUserID)
	})

	client := NewRealClient(oauthCfg, cipher, q)
	_, err = client.CreateEvent(ctx, integrationUserID, EventInput{
		Summary: "should fail",
		StartAt: time.Now().Add(time.Hour),
		EndAt:   time.Now().Add(time.Hour + 30*time.Minute),
	})
	if err == nil {
		t.Fatal("expected error for invalid refresh_token")
	}
	// ErrTokenRevoked で包まれているはず、かつ handleTokenError が
	// UserGoogleToken を削除しているはず
	_, getErr := q.GetUserGoogleToken(ctx, integrationUserID)
	if getErr == nil {
		t.Error("UserGoogleToken should be deleted after invalid_grant")
	}
}
