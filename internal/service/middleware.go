package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bufbuild/connect-go"
)

// contextKey コンテキストのキーとして使用する独自の型
type contextKey string

const (
	userIDKey contextKey = "user_id"
)

// UserInfo JWTペイロードの構造体
type UserInfo struct {
	UserID string `json:"user_id"`
}

// AuthInterceptor JWTペイロードからuser_idを抽出するinterceptor
func AuthInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// X-User-Infoヘッダーからuser_idを抽出
			userInfo, err := extractUserInfo(req)
			if err != nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, err)
			}

			// コンテキストにuser_idを設定
			ctx = context.WithValue(ctx, userIDKey, userInfo.UserID)

			// 次の処理を実行
			return next(ctx, req)
		}
	}
}

// extractUserInfo X-User-Infoヘッダーからuser_idを抽出
func extractUserInfo(req connect.AnyRequest) (*UserInfo, error) {
	// ヘッダーからX-User-Infoを取得
	userInfoHeader := req.Header().Get("X-User-Info")
	if userInfoHeader == "" {
		return nil, fmt.Errorf("X-User-Info header not found")
	}

	// JSONをパース
	var userInfo UserInfo
	if err := json.Unmarshal([]byte(userInfoHeader), &userInfo); err != nil {
		return nil, fmt.Errorf("failed to parse X-User-Info header: %w", err)
	}

	// user_idが空でないことを確認
	if userInfo.UserID == "" {
		return nil, fmt.Errorf("user_id is empty in X-User-Info header")
	}

	return &userInfo, nil
}

// GetUserID コンテキストからuser_idを取得するヘルパー関数
func GetUserID(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDKey).(string)
	return userID, ok
}

// RequireUserID コンテキストからuser_idを取得し、存在しない場合はエラーを返すヘルパー関数
func RequireUserID(ctx context.Context) (string, error) {
	userID, ok := GetUserID(ctx)
	if !ok {
		return "", fmt.Errorf("user_id not found in context")
	}
	return userID, nil
}
