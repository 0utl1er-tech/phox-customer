package book

import (
	"context"
	"fmt"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
)

func (server *BookService) CreateBook(
	ctx context.Context,
	req *connect.Request[bookv1.CreateBookRequest],
) (*connect.Response[bookv1.CreateBookResponse], error) {
	bookId := uuid.New()
	userID := req.Header().Get("X-User-ID")
	if userID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("x-user-idがヘッダーに見つかりません"))
	}

	result, err := server.queries.CreateBook(ctx, db.CreateBookParams{
		ID:   bookId,
		Name: req.Msg.Name,
	})

	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("bookの作成に失敗しました: %w", err))
	}

	_, err = server.queries.CreatePermit(ctx, db.CreatePermitParams{
		ID:     uuid.New(),
		BookID: bookId,
		UserID: userID,
		Role:   db.Role(db.RoleOwner),
	})

	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("permitの作成に失敗しました: %w", err))
	}

	return connect.NewResponse(&bookv1.CreateBookResponse{
		Id: result.ID.String(),
	}), nil
}
