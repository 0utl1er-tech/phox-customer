// Package gcal は Redial を Google Calendar の Event に投影するクライアントを
// 抽象化する。実 GCal API (RealClient) と dev/E2E 用フェイク (MockClient) の
// 2 実装が同じ Client interface を満たす。
//
// factory は config の GCAL_MODE (real|mock) で実装を切り替える。
package gcal

import (
	"context"
	"errors"
	"time"
)

// ErrNotConnected — ユーザーが Google 未連携 (UserGoogleToken が無い)
// ときに返される。サービス層はこれを見て "unsynced" ステータスで Redial を
// 返し、UI にバナーを出す。
var ErrNotConnected = errors.New("gcal: user has not connected Google account")

// EventInput は Redial から計算される Event ペイロード。
type EventInput struct {
	Summary     string
	Description string
	StartAt     time.Time
	EndAt       time.Time
	TimeZone    string
}

// Client は gcal 操作のポート。真の GCal / Mock / 将来のリプレイ版はこれを実装する。
type Client interface {
	// CreateEvent は primary カレンダーに event を作成し、GCal が採番した event_id を返す。
	CreateEvent(ctx context.Context, userID string, in EventInput) (eventID string, err error)
	// PatchEvent は既存 event を更新する。
	PatchEvent(ctx context.Context, userID string, eventID string, in EventInput) error
	// DeleteEvent は event を削除する。既に存在しないなら no-op 相当。
	DeleteEvent(ctx context.Context, userID string, eventID string) error
}
