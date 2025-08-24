package book

import (
	"context"
	"fmt"

	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
)

// GetBooksByUserID ユーザーがアクセス可能なbook一覧を取得
func (s *ServiceImpl) GetBooksByUserID(
	ctx context.Context,
	req *connect.Request[customerv1.GetBooksByUserIDRequest],
) (*connect.Response[customerv1.GetBooksByUserIDResponse], error) {
	// 認証済みユーザーのIDを取得
	userUUID, err := s.requireUserID(ctx)
	if err != nil {
		return nil, err
	}

	// user_idでpermitを検索してbookをJOIN
	books, err := s.queries.GetBooksByUserID(ctx, userUUID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// copierを使ってレスポンス用のbook一覧を作成
	bookList := convertBooksToResponse(books)

	return connect.NewResponse(&customerv1.GetBooksByUserIDResponse{
		Books: bookList,
	}), nil
}

// GetBookByID 特定のbookを取得（ユーザーがアクセス可能な場合のみ）
func (s *ServiceImpl) GetBookByID(
	ctx context.Context,
	req *connect.Request[customerv1.GetBookByIDRequest],
) (*connect.Response[customerv1.GetBookByIDResponse], error) {
	// 認証済みユーザーのIDを取得
	userUUID, err := s.requireUserID(ctx)
	if err != nil {
		return nil, err
	}

	// bookIDをUUIDに変換
	bookUUID, err := s.parseBookID(req.Msg.BookId)
	if err != nil {
		return nil, err
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
		Book: convertBookToResponse(db.Book{
			ID:        book.ID,
			Name:      book.Name,
			CreatedAt: book.CreatedAt,
		}, string(book.Role)),
	}), nil
}
