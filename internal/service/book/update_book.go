package book

import (
	"context"
	"fmt"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
)

func (server *BookService) UpdateBook(
	ctx context.Context,
	req *connect.Request[bookv1.UpdateBookRequest],
) (*connect.Response[bookv1.UpdateBookResponse], error) {
	userID := req.Header().Get("X-User-ID")
	if userID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("X-User-IDがヘッダーに見つかりません"))
	}
	permit, err := server.queries.GetPermitByBookIDAndUserID(ctx, db.GetPermitByBookIDAndUserIDParams{
		BookID: uuid.MustParse(req.Msg.Id),
		UserID: userID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("bookの取得に失敗しました: %w", err))
	}

	if permit.Role != db.RoleOwner {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("bookの更新にはowner権限が必要です"))
	}

	bookId := req.Msg.Id
	bookName := req.Msg.Name

	result, err := server.queries.UpdateBook(ctx, db.UpdateBookParams{
		ID:   uuid.MustParse(bookId),
		Name: bookName,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get book: %w", err))
	}

	return connect.NewResponse(&bookv1.UpdateBookResponse{
		UpdatedBook: &bookv1.Book{
			Id:   result.ID.String(),
			Name: result.Name,
		},
	}), nil
}
