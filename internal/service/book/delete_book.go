package book

import (
	"context"
	"fmt"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"connectrpc.com/connect"
	"github.com/google/uuid"
)

func (s *BookService) DeleteBook(
	ctx context.Context,
	req *connect.Request[bookv1.DeleteBookRequest],
) (*connect.Response[bookv1.DeleteBookResponse], error) {
	bookId, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid book ID format: %w", err))
	}

	if err := s.authorizer.CheckPermission(ctx, bookId, db.RoleOwner); err != nil {
		return nil, err
	}

	err = s.queries.DeleteBook(ctx, bookId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete book: %w", err))
	}
	return connect.NewResponse(&bookv1.DeleteBookResponse{}), nil
}
