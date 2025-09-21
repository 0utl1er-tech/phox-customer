package book

import (
	"context"
	"fmt"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
)

func (server *BookService) DeleteBook(
	ctx context.Context,
	req *connect.Request[bookv1.DeleteBookRequest],
) (*connect.Response[bookv1.DeleteBookResponse], error) {
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
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("bookの削除にはowner権限が必要です"))
	}

	bookId := uuid.MustParse(req.Msg.Id)
	err = server.queries.DeleteBook(ctx, bookId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("bookの削除に失敗しました: %w", err))
	}
	return &connect.Response[bookv1.DeleteBookResponse]{
		Msg: &bookv1.DeleteBookResponse{
			DeletedBook: &bookv1.Book{
				Id: bookId.String(),
			},
		},
	}, nil
}
