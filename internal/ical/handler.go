package ical

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

// Handler は plain HTTP endpoint `GET /ical/{filename}` をホストする。
// Go 1.22+ の path pattern でマッチされた `filename` から `.ics` サフィックスを
// 剥がし、token として扱う。token は URL 自体が credential という設計のため、
// 認証 interceptor を通さない。
type Handler struct {
	q           *db.Queries
	phoxBaseURL string
	// historyWindow は過去何日分の redial を feed に含めるか。デフォ 90 日。
	historyWindow time.Duration
}

// NewHandler returns a ready-to-mount iCal feed handler.
func NewHandler(q *db.Queries, phoxBaseURL string) *Handler {
	return &Handler{
		q:             q,
		phoxBaseURL:   phoxBaseURL,
		historyWindow: 90 * 24 * time.Hour,
	}
}

// Serve は `GET /ical/{filename}` に対応する。
//   - filename から `.ics` を strip → token
//   - 一致する UserICalFeed が無ければ 404
//   - 見つかれば対応 user の redial を JOIN 付きで fetch → feed 生成
//   - ETag match なら 304、match しなければ 200 で text/calendar 返却
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	token := strings.TrimSuffix(filename, ".ics")
	if token == "" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()

	feedRow, err := h.q.FindUserICalFeedByToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		log.Error().Err(err).Msg("ical: feed lookup failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	user, err := h.q.GetUser(ctx, feedRow.UserID)
	if err != nil {
		// Feed row があっても User が消えてる → 整合性破損 → 404
		log.Warn().Err(err).Str("user_id", feedRow.UserID).Msg("ical: user not found")
		http.NotFound(w, r)
		return
	}

	cutoff := time.Now().Add(-h.historyWindow)
	rows, err := h.q.ListRedialsByUserWithCustomer(ctx, db.ListRedialsByUserWithCustomerParams{
		UserID:     feedRow.UserID,
		StartAtMin: cutoff,
	})
	if err != nil {
		log.Error().Err(err).Str("user_id", feedRow.UserID).Msg("ical: redials fetch failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	body := BuildFeed(FeedInput{
		UserID:      user.ID,
		UserName:    user.Name,
		PhoxBaseURL: h.phoxBaseURL,
		Redials:     rows,
		GeneratedAt: time.Now(),
	})

	// ETag: sha256(body) の先頭 16 hex
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		logAccess(r, feedRow.UserID, http.StatusNotModified, len(body))
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="phox.ics"`)
	w.Header().Set("Cache-Control", "private, max-age=900")
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
	logAccess(r, feedRow.UserID, http.StatusOK, len(body))
}

// logAccess は iCal feed endpoint のアクセスログを構造化ログで出す。
// セキュリティ: token は一切出さない。IP は ハッシュ化 (partial anonymization)。
func logAccess(r *http.Request, userID string, status int, bytes int) {
	log.Info().
		Str("endpoint", "ical_feed").
		Str("user_id", userID).
		Int("status", status).
		Int("bytes", bytes).
		Str("ua", r.UserAgent()).
		Str("remote_addr_hash", hashRemoteAddr(r)).
		Msg("ical feed accessed")
}

func hashRemoteAddr(r *http.Request) string {
	addr := r.RemoteAddr
	// strip port
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	h := sha256.Sum256([]byte(addr))
	return hex.EncodeToString(h[:4])
}

