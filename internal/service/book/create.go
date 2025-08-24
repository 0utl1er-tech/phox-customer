package book

import (
	"context"

	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/bufbuild/connect-go"
	"github.com/google/uuid"
)

// CreateBook 新しいbookを作成
func (s *ServiceImpl) CreateBook(
	ctx context.Context,
	req *connect.Request[customerv1.CreateBookRequest],
) (*connect.Response[customerv1.CreateBookResponse], error) {
	// 認証済みユーザーのIDを取得
	userUUID, err := s.requireUserID(ctx)
	if err != nil {
		return nil, err
	}

	// UUIDを生成
	bookID := uuid.New()

	// bookを作成
	err = s.queries.CreateBook(ctx, db.CreateBookParams{
		ID:   bookID,
		Name: req.Msg.Name,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// 作成者にowner権限を付与
	permitID := uuid.New()
	err = s.queries.CreatePermit(ctx, db.CreatePermitParams{
		ID:     permitID,
		BookID: bookID,
		Role:   db.RoleOwner,
		UserID: userUUID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&customerv1.CreateBookResponse{
		Id:   bookID.String(),
		Name: req.Msg.Name,
	}), nil
}
