package gcal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// ErrTokenRevoked は refresh_token が Google 側で失効 (revoked / expired) したことを示す。
// サービス層はこれを見て UserGoogleToken を削除し、UI で再連携を促す。
var ErrTokenRevoked = errors.New("gcal: refresh token revoked or expired; user must reconnect")

// RealClient は実 Google Calendar API を叩く Client 実装。
// ユーザーの refresh_token を UserGoogleToken から取り出し、oauth2.TokenSource
// 経由で自動リフレッシュ + 新しい access_token を DB に永続化する。
type RealClient struct {
	cfg    *oauth2.Config
	cipher *crypto.Cipher
	q      *db.Queries
}

func NewRealClient(cfg *oauth2.Config, cipher *crypto.Cipher, q *db.Queries) *RealClient {
	return &RealClient{cfg: cfg, cipher: cipher, q: q}
}

// tokenSource は UserGoogleToken を復号して oauth2.TokenSource を構築する。
// 戻り値は persistingTokenSource でラップされ、refresh 後の access_token を
// DB に書き戻す。oauth2.Config.TokenSource は内部で ReuseTokenSource を使うので
// 毎回無駄に refresh はされない。
func (c *RealClient) tokenSource(ctx context.Context, userID string) (oauth2.TokenSource, error) {
	row, err := c.q.GetUserGoogleToken(ctx, userID)
	if err != nil {
		return nil, ErrNotConnected
	}
	rt, err := c.cipher.DecryptString(row.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("gcal: decrypt refresh_token: %w", err)
	}
	tok := &oauth2.Token{
		RefreshToken: rt,
	}
	// キャッシュされた access_token があれば (expiry 含め) 再利用して refresh 呼び出しを節約する。
	if len(row.AccessToken) > 0 {
		at, err := c.cipher.DecryptString(row.AccessToken)
		if err == nil {
			tok.AccessToken = at
		}
	}
	if row.Expiry.Valid {
		tok.Expiry = row.Expiry.Time
	}

	return &persistingTokenSource{
		ts:          c.cfg.TokenSource(ctx, tok),
		userID:      userID,
		cipher:      c.cipher,
		q:           c.q,
		lastRefresh: tok.RefreshToken,
		lastAccess:  tok.AccessToken,
	}, nil
}

// persistingTokenSource は oauth2.TokenSource をラップし、refresh 時に
// 新しい access_token / refresh_token (rotation 対応) を DB に書き戻す。
type persistingTokenSource struct {
	ts     oauth2.TokenSource
	userID string
	cipher *crypto.Cipher
	q      *db.Queries

	mu          sync.Mutex
	lastAccess  string
	lastRefresh string
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	t, err := p.ts.Token()
	if err != nil {
		// oauth2 の RetrieveError は invalid_grant / expired を示す。
		// この場合 refresh_token は二度と使えないので、呼び出し側がトークン行を
		// 削除できるように ErrTokenRevoked に包んで返す。
		var rerr *oauth2.RetrieveError
		if errors.As(err, &rerr) {
			body := strings.ToLower(string(rerr.Body))
			if strings.Contains(body, "invalid_grant") ||
				strings.Contains(body, "token has been expired") ||
				strings.Contains(body, "revoked") {
				return nil, ErrTokenRevoked
			}
		}
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	needAccessUpdate := t.AccessToken != "" && t.AccessToken != p.lastAccess
	// Google は通常 refresh_token をローテートしないが、する場合があるので
	// 念のため差分があれば Upsert で refresh_token も更新する。
	needRefreshUpdate := t.RefreshToken != "" && t.RefreshToken != p.lastRefresh

	if !needAccessUpdate && !needRefreshUpdate {
		return t, nil
	}

	ctx := context.Background() // oauth2 TokenSource は ctx を持ち回さない仕様

	if needRefreshUpdate {
		// refresh_token が新しくなったので Upsert で両方書く。
		rtEnc, rerr := p.cipher.EncryptString(t.RefreshToken)
		if rerr != nil {
			log.Warn().Err(rerr).Msg("gcal: encrypt new refresh_token")
			return t, nil
		}
		var atEnc []byte
		if t.AccessToken != "" {
			atEnc, _ = p.cipher.EncryptString(t.AccessToken)
		}
		// 既存行の scopes / google_email を残すため先に取得してから Upsert
		row, gerr := p.q.GetUserGoogleToken(ctx, p.userID)
		scopes := ""
		googleEmail := ""
		if gerr == nil {
			scopes = row.Scopes
			googleEmail = row.GoogleEmail
		}
		_, uerr := p.q.UpsertUserGoogleToken(ctx, db.UpsertUserGoogleTokenParams{
			UserID:       p.userID,
			RefreshToken: rtEnc,
			AccessToken:  atEnc,
			Expiry:       pgtype.Timestamptz{Time: t.Expiry, Valid: !t.Expiry.IsZero()},
			Scopes:       scopes,
			GoogleEmail:  googleEmail,
		})
		if uerr != nil {
			log.Warn().Err(uerr).Msg("gcal: upsert rotated tokens")
		}
		p.lastRefresh = t.RefreshToken
		p.lastAccess = t.AccessToken
		return t, nil
	}

	// access_token のみ更新 (通常経路)
	enc, encErr := p.cipher.EncryptString(t.AccessToken)
	if encErr != nil {
		log.Warn().Err(encErr).Msg("gcal: encrypt new access_token")
		return t, nil
	}
	_, uerr := p.q.UpdateUserGoogleTokenAccess(ctx, db.UpdateUserGoogleTokenAccessParams{
		UserID:      p.userID,
		AccessToken: enc,
		Expiry:      pgtype.Timestamptz{Time: t.Expiry, Valid: !t.Expiry.IsZero()},
	})
	if uerr != nil {
		log.Warn().Err(uerr).Msg("gcal: persist new access_token")
	}
	p.lastAccess = t.AccessToken
	return t, nil
}

func (c *RealClient) newService(ctx context.Context, userID string) (*calendar.Service, error) {
	ts, err := c.tokenSource(ctx, userID)
	if err != nil {
		return nil, err
	}
	svc, err := calendar.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("gcal: new calendar.Service: %w", err)
	}
	return svc, nil
}

// handleTokenError は newService / Event API から返ったエラーを検査し、
// ErrTokenRevoked を検出した場合は UserGoogleToken を削除して呼び出し側に通知する。
func (c *RealClient) handleTokenError(ctx context.Context, userID string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrTokenRevoked) {
		log.Warn().
			Str("user_id", userID).
			Msg("gcal: revoked refresh_token detected — deleting UserGoogleToken so user can reconnect")
		if derr := c.q.DeleteUserGoogleToken(ctx, userID); derr != nil {
			log.Error().Err(derr).Msg("gcal: failed to delete revoked UserGoogleToken")
		}
		return ErrTokenRevoked
	}
	return err
}

func toEvent(in EventInput) *calendar.Event {
	tz := in.TimeZone
	if tz == "" {
		tz = "Asia/Tokyo"
	}
	return &calendar.Event{
		Summary:     in.Summary,
		Description: in.Description,
		Start: &calendar.EventDateTime{
			DateTime: in.StartAt.Format(time.RFC3339),
			TimeZone: tz,
		},
		End: &calendar.EventDateTime{
			DateTime: in.EndAt.Format(time.RFC3339),
			TimeZone: tz,
		},
	}
}

func (c *RealClient) CreateEvent(
	ctx context.Context,
	userID string,
	in EventInput,
) (string, error) {
	svc, err := c.newService(ctx, userID)
	if err != nil {
		return "", c.handleTokenError(ctx, userID, err)
	}
	ev, err := svc.Events.Insert("primary", toEvent(in)).Context(ctx).Do()
	if err != nil {
		return "", c.handleTokenError(ctx, userID, fmt.Errorf("gcal: insert event: %w", err))
	}
	return ev.Id, nil
}

func (c *RealClient) PatchEvent(
	ctx context.Context,
	userID, eventID string,
	in EventInput,
) error {
	svc, err := c.newService(ctx, userID)
	if err != nil {
		return c.handleTokenError(ctx, userID, err)
	}
	if _, err := svc.Events.Patch("primary", eventID, toEvent(in)).Context(ctx).Do(); err != nil {
		// 404/410 は「既に消えてる or まだ無い」なので、連携状態は正常。
		if isEventGone(err) {
			return nil
		}
		return c.handleTokenError(ctx, userID, fmt.Errorf("gcal: patch event: %w", err))
	}
	return nil
}

func (c *RealClient) DeleteEvent(
	ctx context.Context,
	userID, eventID string,
) error {
	svc, err := c.newService(ctx, userID)
	if err != nil {
		// 連携解除後の delete は成功扱い (冪等)。
		if errors.Is(err, ErrNotConnected) {
			return nil
		}
		return c.handleTokenError(ctx, userID, err)
	}
	if err := svc.Events.Delete("primary", eventID).Context(ctx).Do(); err != nil {
		// 既に削除済み or 存在しない event は成功扱い (冪等)。
		if isEventGone(err) {
			return nil
		}
		return c.handleTokenError(ctx, userID, fmt.Errorf("gcal: delete event: %w", err))
	}
	return nil
}

// isEventGone は Google API の 404/410 レスポンスを検出する。
// 「存在しない / 既に削除済み」は冪等性のため成功扱いにする。
func isEventGone(err error) bool {
	if err == nil {
		return false
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		return gerr.Code == http.StatusNotFound || gerr.Code == http.StatusGone
	}
	return false
}
