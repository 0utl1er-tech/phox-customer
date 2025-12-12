package book

import (
	"context"
	"fmt"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *BookService) GetBook(
	ctx context.Context,
	req *connect.Request[bookv1.GetBookRequest],
) (*connect.Response[bookv1.GetBookResponse], error) {
	payload, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	bookID, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid book ID format: %w", err))
	}

	// Check if user has access to the book
	hasAccess, err := s.queries.CheckUserAccessToBook(ctx, db.CheckUserAccessToBookParams{
		BookID: bookID,
		UserID: payload.Subject(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check book access: %w", err))
	}
	if !hasAccess {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("user does not have access to this book"))
	}

	result, err := s.queries.GetBookByIDAndUserID(ctx, db.GetBookByIDAndUserIDParams{
		ID:     bookID,
		UserID: payload.Subject(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get book: %w", err))
	}

	return connect.NewResponse(&bookv1.GetBookResponse{
		Book: &bookv1.Book{
			Id:        result.ID.String(),
			Name:      result.Name,
			CreatedAt: timestamppb.New(result.CreatedAt),
			UpdatedAt: timestamppb.New(result.UpdatedAt),
		},
	}), nil
}
