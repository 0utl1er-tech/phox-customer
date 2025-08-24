package customer

import (
	"context"
	"fmt"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// validateUserAndBookAccess ユーザー認証とbookアクセス権限を検証する共通処理
func (s *ServiceImpl) validateUserAndBookAccess(ctx context.Context, bookIDStr string) (uuid.UUID, uuid.UUID, error) {
	// 認証済みユーザーのIDを取得
	userID, err := service.RequireUserID(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	// userIDをUUIDに変換
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return uuid.Nil, uuid.Nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid user ID format: %w", err))
	}

	// bookIDをUUIDに変換
	bookID, err := uuid.Parse(bookIDStr)
	if err != nil {
		return uuid.Nil, uuid.Nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid book ID format: %w", err))
	}

	// ユーザーがこのbookにアクセス権限があるかを検証
	if err := s.checkUserAccessToBook(ctx, bookID, userUUID); err != nil {
		return uuid.Nil, uuid.Nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("access denied: %w", err))
	}

	return userUUID, bookID, nil
}

// checkUserAccessToBook ユーザーが指定されたbookにアクセス権限があるかを検証
func (s *ServiceImpl) checkUserAccessToBook(ctx context.Context, bookID uuid.UUID, userID uuid.UUID) error {
	hasAccess, err := s.queries.CheckUserAccessToBook(ctx, db.CheckUserAccessToBookParams{
		BookID: bookID,
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("failed to check user access: %w", err)
	}

	if !hasAccess {
		return fmt.Errorf("user does not have access to this book")
	}

	return nil
}

// checkUserRoleForBook ユーザーが指定されたbookで指定されたrole以上を持っているかを検証
func (s *ServiceImpl) checkUserRoleForBook(ctx context.Context, bookID uuid.UUID, userID uuid.UUID, requiredRole db.Role) error {
	userRole, err := s.queries.CheckUserRoleForBook(ctx, db.CheckUserRoleForBookParams{
		BookID: bookID,
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("failed to check user role: %w", err)
	}

	// roleの優先度をチェック（owner > editor > viewer）
	if !s.hasRequiredRole(userRole, requiredRole) {
		return fmt.Errorf("user role '%s' is insufficient for this operation, required: '%s'", userRole, requiredRole)
	}

	return nil
}

// hasRequiredRole ユーザーのroleが要求されるrole以上かどうかをチェック
func (s *ServiceImpl) hasRequiredRole(userRole, requiredRole db.Role) bool {
	roleHierarchy := map[db.Role]int{
		db.RoleViewer: 1,
		db.RoleEditor: 2,
		db.RoleOwner:  3,
	}

	userLevel, userExists := roleHierarchy[userRole]
	requiredLevel, requiredExists := roleHierarchy[requiredRole]

	if !userExists || !requiredExists {
		return false
	}

	return userLevel >= requiredLevel
}

// createCategoryIfNeeded カテゴリが指定されている場合にUpsert処理を実行
func (s *ServiceImpl) createCategoryIfNeeded(ctx context.Context, categoryName string, bookID uuid.UUID) (pgtype.UUID, error) {
	if categoryName == "" {
		return pgtype.UUID{Valid: false}, nil
	}

	category, err := s.queries.UpsertCategory(ctx, db.UpsertCategoryParams{
		ID:     uuid.New(),
		BookID: bookID,
		Name:   categoryName,
	})
	if err != nil {
		return pgtype.UUID{Valid: false}, err
	}

	return pgtype.UUID{
		Bytes: category.ID,
		Valid: true,
	}, nil
}
