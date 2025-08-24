package book

import (
	"context"
	"fmt"

	"github.com/0utl1er-tech/phox-customer/internal/service"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
)

// requireUserID 認証済みユーザーのIDを取得する共通処理
func (s *ServiceImpl) requireUserID(ctx context.Context) (uuid.UUID, error) {
	// 認証済みユーザーのIDを取得
	userID, err := service.RequireUserID(ctx)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	// userIDをUUIDに変換
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid user ID format: %w", err))
	}

	return userUUID, nil
}

// parseBookID bookIDをUUIDに変換する共通処理
func (s *ServiceImpl) parseBookID(bookIDStr string) (uuid.UUID, error) {
	bookUUID, err := uuid.Parse(bookIDStr)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid book ID format: %w", err))
	}
	return bookUUID, nil
}
