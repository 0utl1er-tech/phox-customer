package service

import (
	"context"
	"fmt"

	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
)

type BookService struct {
	queries *db.Queries
}

func NewBookService(queries *db.Queries) *BookService {
	return &BookService{
		queries: queries,
	}
}

// GetBooksByUserID ユーザーがアクセス可能なbook一覧を取得
func (s *BookService) GetBooksByUserID(
	ctx context.Context,
	req *connect.Request[customerv1.GetBooksByUserIDRequest],
) (*connect.Response[customerv1.GetBooksByUserIDResponse], error) {
	// 認証済みユーザーのIDを取得
	userID, err := RequireUserID(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	// userIDをUUIDに変換
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid user ID format: %w", err))
	}

	// user_idでpermitを検索してbookをJOIN
	books, err := s.queries.GetBooksByUserID(ctx, userUUID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// レスポンス用のbook一覧を作成
	var bookList []*customerv1.Book
	for _, book := range books {
		bookList = append(bookList, &customerv1.Book{
			Id:        book.ID.String(),
			Name:      book.Name,
			Role:      string(book.Role),
			CreatedAt: book.CreatedAt.String(),
		})
	}

	return connect.NewResponse(&customerv1.GetBooksByUserIDResponse{
		Books: bookList,
	}), nil
}

// GetBookByID 特定のbookを取得（ユーザーがアクセス可能な場合のみ）
func (s *BookService) GetBookByID(
	ctx context.Context,
	req *connect.Request[customerv1.GetBookByIDRequest],
) (*connect.Response[customerv1.GetBookByIDResponse], error) {
	// 認証済みユーザーのIDを取得
	userID, err := RequireUserID(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	// userIDをUUIDに変換
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid user ID format: %w", err))
	}

	bookID := req.Msg.BookId

	// bookIDをUUIDに変換
	bookUUID, err := uuid.Parse(bookID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid book ID format: %w", err))
	}

	// book_idとuser_idでpermitを検索してbookをJOIN
	book, err := s.queries.GetBookByIDAndUserID(ctx, db.GetBookByIDAndUserIDParams{
		ID:     bookUUID,
		UserID: userUUID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("book not found or access denied: %w", err))
	}

	return connect.NewResponse(&customerv1.GetBookByIDResponse{
		Book: &customerv1.Book{
			Id:        book.ID.String(),
			Name:      book.Name,
			Role:      string(book.Role),
			CreatedAt: book.CreatedAt.String(),
		},
	}), nil
}

// CreateBook 新しいbookを作成
func (s *BookService) CreateBook(
	ctx context.Context,
	req *connect.Request[customerv1.CreateBookRequest],
) (*connect.Response[customerv1.CreateBookResponse], error) {
	// 認証済みユーザーのIDを取得
	userID, err := RequireUserID(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	// userIDをUUIDに変換
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid user ID format: %w", err))
	}

	// UUIDを生成
	bookID := uuid.New()

	// bookを作成
	err = s.queries.CreateBook(ctx, db.CreateBookParams{
		ID:   bookID,
		Name: req.Msg.Name,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// 作成者にowner権限を付与
	permitID := uuid.New()
	err = s.queries.CreatePermit(ctx, db.CreatePermitParams{
		ID:     permitID,
		BookID: bookID,
		Role:   db.RoleOwner,
		UserID: userUUID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&customerv1.CreateBookResponse{
		Id:   bookID.String(),
		Name: req.Msg.Name,
	}), nil
}
