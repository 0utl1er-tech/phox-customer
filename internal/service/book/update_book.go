package book

import (
	"context"
	"fmt"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"connectrpc.com/connect"
	"github.com/google/uuid"
)

func (s *BookService) UpdateBook(
	ctx context.Context,
	req *connect.Request[bookv1.UpdateBookRequest],
) (*connect.Response[bookv1.UpdateBookResponse], error) {
	bookId, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid book ID format: %w", err))
	}

	if err := s.authorizer.CheckPermission(ctx, bookId, db.RoleOwner); err != nil {
		return nil, err
	}

	result, err := s.queries.UpdateBook(ctx, db.UpdateBookParams{
		ID:   bookId,
		Name: req.Msg.Name,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update book: %w", err))
	}

	return connect.NewResponse(&bookv1.UpdateBookResponse{
		UpdatedBook: &bookv1.Book{
			Id:   result.ID.String(),
			Name: result.Name,
		},
	}), nil
}
