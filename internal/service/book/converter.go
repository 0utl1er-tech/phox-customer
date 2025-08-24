package book

import (
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/jinzhu/copier"
)

// convertBookToResponse DBのBookをレスポンス用のBookに変換（copier使用）
func convertBookToResponse(book db.Book, role string) *customerv1.Book {
	var response customerv1.Book
	
	// copierを使って構造体をコピー
	if err := copier.Copy(&response, &book); err != nil {
		// エラーが発生した場合は手動で変換
		return &customerv1.Book{
			Id:        book.ID.String(),
			Name:      book.Name,
			Role:      role,
			CreatedAt: book.CreatedAt.String(),
		}
	}

	// UUIDフィールドを文字列に変換
	response.Id = book.ID.String()
	response.Role = role
	response.CreatedAt = book.CreatedAt.String()

	return &response
}

// convertBooksToResponse DBのBook一覧をレスポンス用のBook一覧に変換
func convertBooksToResponse(books []db.GetBooksByUserIDRow) []*customerv1.Book {
	var bookList []*customerv1.Book
	for _, book := range books {
		bookList = append(bookList, &customerv1.Book{
			Id:        book.ID.String(),
			Name:      book.Name,
			Role:      string(book.Role),
			CreatedAt: book.CreatedAt.String(),
		})
	}
	return bookList
}
