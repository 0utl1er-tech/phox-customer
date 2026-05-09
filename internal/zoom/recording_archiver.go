package zoom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rs/zerolog/log"
)

// RecordingArchiver は Zoom Phone の通話録音 (download_url) を取得して
// Ceph RGW (S3 互換) の永続バケットに保存する。Activity.recording_url には
// `s3://<bucket>/<key>` 形式のパスを保存する (UI 再生時は API 越しに
// presigned GET URL を発行)。
//
// Zoom 側の保管期間は無料枠で 30 日 → 自前で長期保存しないと Activity に
// 残った URL が無効化される。RGW lifecycle で 1 年以降 expire させる
// 方針は `phox/manifests/45-recording-bucket.yaml` 側で管理。
//
// nil 受け取り対応: dev / config 未設定環境では Archiver を構築せず
// nil で受け渡し、Archive() 内で no-op 化する (= recording_url 空のまま)。
type RecordingArchiver struct {
	zoomClient *Client
	s3         *minio.Client
	bucket     string
}

// NewRecordingArchiver は RGW endpoint と OBC 由来の credentials で
// MinIO client を立て、archiver を返す。endpoint / bucket / key が空なら
// nil を返す (= recording archive 機能 disabled)。
func NewRecordingArchiver(
	zoomClient *Client,
	endpoint, accessKey, secretKey, bucket, region string,
	useTLS bool,
) (*RecordingArchiver, error) {
	if zoomClient == nil {
		return nil, nil // Zoom 自体 disabled なら archiver も無し
	}
	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		return nil, nil // OBC 未 provision / config 未設定
	}
	// minio-go は scheme 無しの host:port を期待 (`http://` を剥がす)
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	if region == "" {
		region = "us-east-1" // Ceph RGW は region 概念無いがダミー必須
	}
	c, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useTLS,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("recording archiver: minio init: %w", err)
	}
	return &RecordingArchiver{zoomClient: zoomClient, s3: c, bucket: bucket}, nil
}

// Enabled は archive 可能な状態か (= NewRecordingArchiver で nil 以外の
// instance が返ったか) を表す。webhook handler が pre-check する。
func (a *RecordingArchiver) Enabled() bool {
	return a != nil && a.s3 != nil && a.bucket != ""
}

// S3 は内部の minio client を返す。再生用 signed URL を発行するパッケージ
// (internal/recording) が同一 client を再利用するためのアクセサ。
// archiver が disabled なら nil を返す。
func (a *RecordingArchiver) S3() *minio.Client {
	if !a.Enabled() {
		return nil
	}
	return a.s3
}

// Archive は callID の通話録音を Zoom から download → S3 に PUT する。
// 成功時 `s3://<bucket>/<key>` 形式のパスを返す (DB 保存用)。
//
// downloadURL は Zoom webhook の `phone.recording_completed` payload に含まれる
// 短期 URL (15 分有効、Bearer token 必須)。Bearer は Client が管理する
// access_token を再利用する (S2S OAuth)。
func (a *RecordingArchiver) Archive(ctx context.Context, callID, downloadURL string) (string, error) {
	if !a.Enabled() {
		return "", errors.New("recording archiver: disabled")
	}
	if callID == "" || downloadURL == "" {
		return "", errors.New("recording archiver: empty callID or downloadURL")
	}

	tok, err := a.zoomClient.token()
	if err != nil {
		return "", fmt.Errorf("recording archiver: zoom token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("recording archiver: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("recording archiver: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("recording archiver: download status %d: %s",
			resp.StatusCode, string(body))
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "audio/mp4" // Zoom Phone 録音のデフォは mp4 (m4a)
	}
	contentLength := resp.ContentLength // -1 if unknown — stream として扱える

	// S3 key 命名: calls/{call_id}/recording.{ext}
	// Zoom の Content-Type が audio/mp4 → m4a, audio/mpeg → mp3 等。
	ext := "m4a"
	switch contentType {
	case "audio/mpeg":
		ext = "mp3"
	case "audio/wav", "audio/wave":
		ext = "wav"
	}
	key := fmt.Sprintf("calls/%s/recording.%s", callID, ext)

	_, err = a.s3.PutObject(ctx, a.bucket, key, resp.Body, contentLength,
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("recording archiver: s3 put: %w", err)
	}

	s3path := fmt.Sprintf("s3://%s/%s", a.bucket, key)
	log.Info().
		Str("call_id", callID).
		Str("s3_path", s3path).
		Int64("bytes", contentLength).
		Msg("zoom: recording archived")
	return s3path, nil
}
