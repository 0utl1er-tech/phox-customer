package book

import (
	"context"
	"fmt"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"connectrpc.com/connect"
)

func (s *BookService) GetBook(
	ctx context.Context,
	req *connect.Request[bookv1.GetBookRequest],
) (*connect.Response[bookv1.GetBookResponse], error) {
	payload, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	results, err := s.queries.GetBooksByUserID(ctx, payload.Subject())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get books: %w", err))
	}

	books := make([]*bookv1.Book, 0, len(results))
	for _, b := range results {
		books = append(books, &bookv1.Book{
			Id:   b.ID.String(),
			Name: b.Name,
		})
	}

	return connect.NewResponse(&bookv1.GetBookResponse{
		Books: books,
	}), nil
}
