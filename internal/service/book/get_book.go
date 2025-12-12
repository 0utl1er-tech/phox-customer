package book

import (
	"context"
	"fmt"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *BookService) ListBooks(
	ctx context.Context,
	req *connect.Request[bookv1.ListBooksRequest],
) (*connect.Response[bookv1.ListBooksResponse], error) {
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
			Id:        b.ID.String(),
			Name:      b.Name,
			CreatedAt: timestamppb.New(b.CreatedAt),
			UpdatedAt: timestamppb.New(b.UpdatedAt),
		})
	}

	return connect.NewResponse(&bookv1.ListBooksResponse{
		Books: books,
	}), nil
}
