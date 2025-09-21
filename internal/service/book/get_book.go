package book

import (
	"context"
	"fmt"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	"github.com/bufbuild/connect-go"
)

func (server *BookService) GetBook(ctx context.Context, req *connect.Request[bookv1.GetBookRequest]) (*connect.Response[bookv1.GetBookResponse], error) {
	userID := req.Header().Get("X-User-ID")
	if userID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("X-User-IDがヘッダーに見つかりません"))
	}

	book, err := server.queries.GetBooksByUserID(ctx, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get book: %w", err))
	}

	if len(book) == 0 {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("bookが見つかりません"))
	}

	books := make([]*bookv1.Book, len(book))
	for _, b := range book {
		books = append(books, &bookv1.Book{
			Id:   b.ID.String(),
			Name: b.Name,
		})
	}

	return connect.NewResponse(&bookv1.GetBookResponse{
		Books: books,
	}), nil
}
