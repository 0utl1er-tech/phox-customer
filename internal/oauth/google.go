// Package oauth は Google Calendar 連携のための OAuth 2.0 コードフローを
// phox-customer 自身で管理する HTTP handler を提供する。
//
// エンドポイント:
//   - POST /oauth/google/start    — 認証済み (Bearer) → {auth_url} を返す
//   - GET  /oauth/google/callback — Google からのリダイレクトを受け、
//                                    code を exchange して UserGoogleToken を upsert
//
// mock mode (GCAL_MODE=mock):
//   - start は Google に飛ばずに直接 UserGoogleToken を dummy 値で upsert
//   - 返す auth_url は phox-ui の `/settings?google=connected` に直接飛ぶ
package oauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"
	calendar "google.golang.org/api/calendar/v3"
)

// Handler は 2 つの HTTP endpoint をホストする。
type Handler struct {
	config  *oauth2.Config
	cfg     util.Config
	cipher  *crypto.Cipher
	q       *db.Queries
	// stateSecret は state (HMAC) の署名鍵。derived from GCAL_TOKEN_KEY。
	stateSecret []byte
	// jwkCache は Keycloak 公開鍵 (start endpoint で Bearer を検証する時に使う)。
	jwkCache *jwk.Cache
}

func NewHandler(cfg util.Config, cipher *crypto.Cipher, q *db.Queries) (*Handler, error) {
	h := &Handler{
		cfg:    cfg,
		cipher: cipher,
		q:      q,
	}
	// stateSecret: GCAL_TOKEN_KEY (base64) をそのまま流用 (用途は異なるが鍵は共有でも安全)
	raw, err := base64.StdEncoding.DecodeString(cfg.GCalTokenKey)
	if err != nil {
		raw = []byte(cfg.GCalTokenKey)
	}
	h.stateSecret = raw

	// oauth2.Config — Google 公式 endpoint 定数 (googleoauth.Endpoint) を使う。
	// scope は calendar.events (イベント CRUD のみ) + userinfo.email (google_email の取得用)。
	h.config = &oauth2.Config{
		ClientID:     cfg.GoogleOAuthClientID,
		ClientSecret: cfg.GoogleOAuthSecret,
		RedirectURL:  cfg.GoogleOAuthRedirect,
		Scopes: []string{
			calendar.CalendarEventsScope,
			"https://www.googleapis.com/auth/userinfo.email",
		},
		Endpoint: googleoauth.Endpoint,
	}

	// JWK cache for verifying user's Bearer token on POST /oauth/google/start.
	// Re-use Keycloak JWKS — same as the Connect interceptor.
	if cfg.JWTEnabled {
		bgCtx := context.Background()
		cache := jwk.NewCache(bgCtx)
		if err := cache.Register(cfg.JWTJwksURL, jwk.WithMinRefreshInterval(15*time.Minute)); err != nil {
			return nil, fmt.Errorf("oauth: jwk cache register: %w", err)
		}
		if _, err := cache.Refresh(bgCtx, cfg.JWTJwksURL); err != nil {
			// non-fatal — we log and continue; verifyBearer will retry.
			log.Warn().Err(err).Msg("oauth: initial jwk refresh failed")
		}
		h.jwkCache = cache
	}

	return h, nil
}

// signState は HMAC-SHA256(payload) を hex で返す。payload は "user_id|nonce|exp_unix"。
func (h *Handler) signState(userID string) (string, error) {
	exp := time.Now().Add(10 * time.Minute).Unix()
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	body := fmt.Sprintf("%s|%s|%d", userID, nonce, exp)
	mac := hmac.New(sha256.New, h.stateSecret)
	mac.Write([]byte(body))
	sig := hex.EncodeToString(mac.Sum(nil))
	return body + "|" + sig, nil
}

// parseState は signState の逆。有効なら user_id を返す。
func (h *Handler) parseState(s string) (string, error) {
	parts := strings.Split(s, "|")
	if len(parts) != 4 {
		return "", fmt.Errorf("oauth: state has %d parts, want 4", len(parts))
	}
	userID, nonce, expStr, sig := parts[0], parts[1], parts[2], parts[3]
	body := fmt.Sprintf("%s|%s|%s", userID, nonce, expStr)
	mac := hmac.New(sha256.New, h.stateSecret)
	mac.Write([]byte(body))
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return "", fmt.Errorf("oauth: state signature mismatch")
	}
	var exp int64
	if _, err := fmt.Sscanf(expStr, "%d", &exp); err != nil {
		return "", fmt.Errorf("oauth: parse exp: %w", err)
	}
	if time.Now().Unix() > exp {
		return "", fmt.Errorf("oauth: state expired")
	}
	return userID, nil
}

// verifyBearer は Authorization: Bearer <jwt> から user_id (sub) を抽出する。
func (h *Handler) verifyBearer(ctx context.Context, r *http.Request) (string, error) {
	authz := r.Header.Get("Authorization")
	if authz == "" {
		return "", fmt.Errorf("missing authorization header")
	}
	fields := strings.Fields(authz)
	if len(fields) != 2 || strings.ToLower(fields[0]) != "bearer" {
		return "", fmt.Errorf("bad authorization format")
	}
	if !h.cfg.JWTEnabled {
		// dev-only fallback: trust the token and decode the subject without verify.
		tok, err := jwt.Parse([]byte(fields[1]), jwt.WithVerify(false), jwt.WithValidate(false))
		if err != nil {
			return "", fmt.Errorf("parse token: %w", err)
		}
		return tok.Subject(), nil
	}
	keySet, err := h.jwkCache.Get(ctx, h.cfg.JWTJwksURL)
	if err != nil {
		return "", fmt.Errorf("fetch jwks: %w", err)
	}
	tok, err := jwt.Parse([]byte(fields[1]),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
		jwt.WithIssuer(h.cfg.JWTIssuerURL),
		jwt.WithAudience(h.cfg.JWTProjectID),
	)
	if err != nil {
		return "", fmt.Errorf("verify token: %w", err)
	}
	return tok.Subject(), nil
}

// StartHandler (POST /oauth/google/start) — 認証済みユーザーに対して Google 認可
// URL を返す。mock mode では即座に UserGoogleToken を upsert し、
// phox-ui の /settings?google=connected を指す URL を返して即「連携完了」にする。
func (h *Handler) StartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodOptions {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	userID, err := h.verifyBearer(r.Context(), r)
	if err != nil {
		log.Warn().Err(err).Msg("oauth/start: bearer verify failed")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.cfg.GCalMode == "mock" {
		// mock mode — 直接 UserGoogleToken を upsert してユーザー体験として
		// 「連携済み」にする。refresh_token は fake 値 + 暗号化。
		enc, err := h.cipher.EncryptString(fmt.Sprintf("mock-refresh-%s", userID))
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		_, err = h.q.UpsertUserGoogleToken(r.Context(), db.UpsertUserGoogleTokenParams{
			UserID:       userID,
			RefreshToken: enc,
			AccessToken:  nil,
			Expiry:       pgtype.Timestamptz{Valid: false},
			Scopes:       strings.Join(h.config.Scopes, " "),
			GoogleEmail:  fmt.Sprintf("mock-%s@gcal.test", userID),
		})
		if err != nil {
			log.Error().Err(err).Msg("oauth/start: mock upsert failed")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{
			"auth_url": fmt.Sprintf("%s/settings?google=connected", h.cfg.PhoxBaseURL),
		})
		return
	}

	state, err := h.signState(userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	authURL := h.config.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	writeJSON(w, map[string]string{"auth_url": authURL})
}

// CallbackHandler (GET /oauth/google/callback) — Google からのリダイレクトを受け、
// code を exchange → UserGoogleToken を upsert → phox-ui にリダイレクト。
// エラー時は HTTP error ではなく /settings?google=error&reason=... にリダイレクトして
// ユーザー体験を損なわないようにする。
func (h *Handler) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	redirectError := func(reason string) {
		http.Redirect(w, r,
			fmt.Sprintf("%s/settings?google=error&reason=%s", h.cfg.PhoxBaseURL, reason),
			http.StatusFound)
	}

	// Google が consent 拒否を返した場合 (例: error=access_denied)
	if gErr := r.URL.Query().Get("error"); gErr != "" {
		log.Warn().Str("google_error", gErr).Msg("oauth/callback: google returned error")
		redirectError(gErr)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		redirectError("missing_code_or_state")
		return
	}
	userID, err := h.parseState(state)
	if err != nil {
		log.Warn().Err(err).Msg("oauth/callback: invalid state")
		redirectError("invalid_state")
		return
	}

	tok, err := h.config.Exchange(r.Context(), code)
	if err != nil {
		log.Error().Err(err).Msg("oauth/callback: exchange failed")
		redirectError("exchange_failed")
		return
	}
	if tok.RefreshToken == "" {
		// consent 画面で prompt=consent を毎回指定しているので基本的には refresh_token が
		// 返るはず。返らない場合はユーザーが一度同意解除してから再同意していない可能性
		// (そのときは同じ Google アカウントの「Third-party apps」設定から本アプリを削除して
		//  もう一度試す必要がある)。ここでは error redirect で戻す。
		log.Error().Msg("oauth/callback: no refresh token in response — user must revoke and re-authorize")
		redirectError("no_refresh_token")
		return
	}

	// Google userinfo で google_email を取得 (失敗しても致命的ではない)
	googleEmail := ""
	if ui, uerr := fetchGoogleUserEmail(r.Context(), h.config, tok); uerr == nil {
		googleEmail = ui
	} else {
		log.Warn().Err(uerr).Msg("oauth/callback: userinfo fetch failed (non-fatal)")
	}

	rtEnc, err := h.cipher.EncryptString(tok.RefreshToken)
	if err != nil {
		log.Error().Err(err).Msg("oauth/callback: encrypt refresh_token")
		redirectError("encrypt_failed")
		return
	}
	var atEnc []byte
	if tok.AccessToken != "" {
		atEnc, _ = h.cipher.EncryptString(tok.AccessToken)
	}

	_, err = h.q.UpsertUserGoogleToken(r.Context(), db.UpsertUserGoogleTokenParams{
		UserID:       userID,
		RefreshToken: rtEnc,
		AccessToken:  atEnc,
		Expiry:       pgtype.Timestamptz{Time: tok.Expiry, Valid: !tok.Expiry.IsZero()},
		Scopes:       strings.Join(h.config.Scopes, " "),
		GoogleEmail:  googleEmail,
	})
	if err != nil {
		log.Error().Err(err).Msg("oauth/callback: upsert failed")
		redirectError("db_error")
		return
	}

	log.Info().
		Str("user_id", userID).
		Str("google_email", googleEmail).
		Msg("oauth/callback: user connected Google account successfully")

	http.Redirect(w, r, fmt.Sprintf("%s/settings?google=connected", h.cfg.PhoxBaseURL), http.StatusFound)
}

func fetchGoogleUserEmail(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) (string, error) {
	client := cfg.Client(ctx, tok)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v3/userinfo")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("userinfo status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var ui struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &ui); err != nil {
		return "", err
	}
	return ui.Email, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
