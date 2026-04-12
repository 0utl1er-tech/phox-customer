package mailtemplate

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mailtemplatev1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailtemplate/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
)

// CreateMailTemplate は指定 Book にテンプレを新規作成する。
// 認可: Book に editor 以上の権限が必要。
func (s *MailTemplateService) CreateMailTemplate(
	ctx context.Context,
	req *connect.Request[mailtemplatev1.CreateMailTemplateRequest],
) (*connect.Response[mailtemplatev1.CreateMailTemplateResponse], error) {
	bookID, err := uuid.Parse(req.Msg.BookId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid book_id: %w", err))
	}

	if err := s.authorizer.CheckPermission(ctx, bookID, db.RoleEditor); err != nil {
		return nil, err
	}

	tpl, err := s.queries.CreateMailTemplate(ctx, db.CreateMailTemplateParams{
		ID:      uuid.New(),
		BookID:  bookID,
		Name:    req.Msg.Name,
		Subject: req.Msg.Subject,
		Body:    req.Msg.Body,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create mail template: %w", err))
	}

	return connect.NewResponse(&mailtemplatev1.CreateMailTemplateResponse{
		Template: modelToProto(tpl),
	}), nil
}
