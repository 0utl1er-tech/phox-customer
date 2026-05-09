// Package recording は通話録音 (Activity.recording_url = s3://...) を
// ブラウザで再生するための短命 signed URL の発行と、その URL に紐づく
// HTTP ストリーミングハンドラを提供する。
//
// 流れ:
//  1. UI が ActivityService.GetActivityRecording(id) を呼ぶ
//  2. server: 認可確認 → s3path 取得 → SignedURL を発行
//  3. UI が <audio src="<signed URL>"> を生成
//  4. browser が GET /recordings/{id}?exp=&sig= を叩く
//  5. handler: HMAC + expiry 検証 → S3 オブジェクトを stream
//
// 署名は HMAC-SHA256(signing_key, "{activity_id}|{exp_unix}") の hex。
// presigned S3 URL を直接返さないのは、Ceph RGW が cluster 内 service だけで
// browser から到達できないため。
package recording

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/rs/zerolog/log"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

// URLTTL は GetActivityRecording で発行する signed URL の有効期限。
// 短すぎると <audio> の再生開始前に期限切れする可能性があるので
// 余裕を持たせる。長すぎると盗まれた URL の悪用リスクが上がる。
const URLTTL = 5 * time.Minute

// Service は録音 URL の発行と stream を担当する。disabled モード (signing key 不在
// or s3 client 不在) では IsEnabled() が false を返し、RPC 側で CodeUnavailable
// に丸める。
type Service struct {
	queries    *db.Queries
	authorizer *auth.Authorizer
	s3         *minio.Client // nil で disabled
	signingKey []byte        // 空で disabled
	publicBase string        // signed URL の host base (末尾スラッシュ無し)
}

// NewService は依存を組み立てる。s3 が nil または signingKey が空なら disabled。
// publicBase は signed URL を構築する際に "<publicBase>/recordings/<id>?..." の形で使う。
func NewService(
	queries *db.Queries,
	authorizer *auth.Authorizer,
	s3 *minio.Client,
	signingKey, publicBase string,
) *Service {
	return &Service{
		queries:    queries,
		authorizer: authorizer,
		s3:         s3,
		signingKey: []byte(signingKey),
		publicBase: strings.TrimRight(publicBase, "/"),
	}
}

// IsEnabled は録音再生機能が利用可能かを返す。signing key 未設定 / s3 client 不在
// (= 録音 archive 自体が disabled の環境) では false。
func (s *Service) IsEnabled() bool {
	return s != nil && s.s3 != nil && len(s.signingKey) > 0
}

// IssueSignedURL は activity_id に対する短命 GET URL を発行する。
// 呼び出し元は事前に user の auth / permit を確認していること (この関数は
// 認可しない — RPC 側で済ませる)。activity が存在しない / 録音が無い場合は
// pgx.ErrNoRows / errNoRecording を返す。
func (s *Service) IssueSignedURL(ctx context.Context, activityID uuid.UUID) (signedURL string, expiresAt time.Time, err error) {
	if !s.IsEnabled() {
		return "", time.Time{}, ErrDisabled
	}

	a, err := s.queries.GetActivity(ctx, activityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", time.Time{}, ErrActivityNotFound
		}
		return "", time.Time{}, fmt.Errorf("recording: get activity: %w", err)
	}
	if !a.RecordingUrl.Valid || a.RecordingUrl.String == "" {
		return "", time.Time{}, ErrNoRecording
	}

	exp := time.Now().Add(URLTTL)
	sig := s.sign(activityID.String(), exp.Unix())

	q := url.Values{}
	q.Set("exp", strconv.FormatInt(exp.Unix(), 10))
	q.Set("sig", sig)
	return fmt.Sprintf("%s/recordings/%s?%s", s.publicBase, activityID.String(), q.Encode()), exp, nil
}

// ServeHTTP は <audio src="..."> から呼ばれる streaming endpoint。
// 入口で URL 署名と expiry を検証 → S3 から fetch → pipe response。
// 失敗時は HTTP error を直接書く (Connect-RPC ではなく素の HTTP)。
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.IsEnabled() {
		http.Error(w, "recording: disabled", http.StatusServiceUnavailable)
		return
	}

	// path: /recordings/{activity_id}
	id := strings.TrimPrefix(r.URL.Path, "/recordings/")
	id = strings.TrimSuffix(id, "/")
	activityID, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "invalid activity id", http.StatusBadRequest)
		return
	}

	expStr := r.URL.Query().Get("exp")
	sig := r.URL.Query().Get("sig")
	if expStr == "" || sig == "" {
		http.Error(w, "missing exp/sig", http.StatusBadRequest)
		return
	}
	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid exp", http.StatusBadRequest)
		return
	}
	if time.Now().Unix() > expUnix {
		http.Error(w, "url expired", http.StatusGone)
		return
	}
	expected := s.sign(activityID.String(), expUnix)
	// constant-time compare to avoid timing leaks on the HMAC
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}

	a, err := s.queries.GetActivity(r.Context(), activityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Warn().Err(err).Str("activity_id", id).Msg("recording: lookup failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !a.RecordingUrl.Valid || a.RecordingUrl.String == "" {
		http.Error(w, "no recording", http.StatusNotFound)
		return
	}

	bucket, key, err := parseS3URL(a.RecordingUrl.String)
	if err != nil {
		log.Warn().Err(err).Str("recording_url", a.RecordingUrl.String).Msg("recording: bad s3 path")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	obj, err := s.s3.GetObject(r.Context(), bucket, key, minio.GetObjectOptions{})
	if err != nil {
		log.Warn().Err(err).Msg("recording: s3 get object")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer obj.Close()

	// Stat の失敗は object 不在を含むので 404 に丸める。
	stat, err := obj.Stat()
	if err != nil {
		log.Warn().Err(err).Str("bucket", bucket).Str("key", key).Msg("recording: s3 stat")
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	contentType := stat.ContentType
	if contentType == "" {
		contentType = "audio/mp4"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size, 10))
	// signed URL は public な代わりに短命なので、CDN/Browser cache を抑止。
	w.Header().Set("Cache-Control", "private, no-cache, no-store, max-age=0")

	if _, err := io.Copy(w, obj); err != nil {
		// client disconnect 等の伝播エラーは debug で。
		log.Debug().Err(err).Str("activity_id", id).Msg("recording: stream copy")
	}
}

func (s *Service) sign(activityID string, expUnix int64) string {
	h := hmac.New(sha256.New, s.signingKey)
	fmt.Fprintf(h, "%s|%d", activityID, expUnix)
	return hex.EncodeToString(h.Sum(nil))
}

// parseS3URL は "s3://<bucket>/<key>" から bucket と key を取り出す。
// archive 側で書き込んだ形式 (recording_archiver.go) と対称。
func parseS3URL(s3url string) (bucket, key string, err error) {
	if !strings.HasPrefix(s3url, "s3://") {
		return "", "", fmt.Errorf("not an s3 url: %q", s3url)
	}
	rest := strings.TrimPrefix(s3url, "s3://")
	idx := strings.IndexByte(rest, '/')
	if idx < 0 {
		return "", "", fmt.Errorf("missing key in s3 url: %q", s3url)
	}
	return rest[:idx], rest[idx+1:], nil
}

// 内部エラー型 — RPC 側で connect.Code に変換する。
var (
	ErrDisabled         = errors.New("recording service disabled")
	ErrActivityNotFound = errors.New("activity not found")
	ErrNoRecording      = errors.New("activity has no recording")
)
