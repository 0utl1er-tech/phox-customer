package book

import (
	"context"
	"fmt"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"connectrpc.com/connect"
	"github.com/google/uuid"
)

func (s *BookService) CreateBook(
	ctx context.Context,
	req *connect.Request[bookv1.CreateBookRequest],
) (*connect.Response[bookv1.CreateBookResponse], error) {
	payload, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	bookId := uuid.New()

	// TODO: Execute in a single transaction
	result, err := s.queries.CreateBook(ctx, db.CreateBookParams{
		ID:   bookId,
		Name: req.Msg.Name,
	})

	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create book: %w", err))
	}

	_, err = s.queries.CreatePermit(ctx, db.CreatePermitParams{
		ID:     uuid.New(),
		BookID: bookId,
		UserID: payload.Subject(),
		Role:   db.RoleOwner,
	})

	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create permit: %w", err))
	}

	return connect.NewResponse(&bookv1.CreateBookResponse{
		Id: result.ID.String(),
	}), nil
}
