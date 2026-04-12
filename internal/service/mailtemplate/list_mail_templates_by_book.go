package mailtemplate

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mailtemplatev1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailtemplate/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
)

// ListMailTemplatesByBook は Book 内の全テンプレを作成日時降順で返す。
// 認可: Book に viewer 以上の権限が必要。
func (s *MailTemplateService) ListMailTemplatesByBook(
	ctx context.Context,
	req *connect.Request[mailtemplatev1.ListMailTemplatesByBookRequest],
) (*connect.Response[mailtemplatev1.ListMailTemplatesByBookResponse], error) {
	bookID, err := uuid.Parse(req.Msg.BookId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid book_id: %w", err))
	}

	if err := s.authorizer.CheckPermission(ctx, bookID, db.RoleViewer); err != nil {
		return nil, err
	}

	rows, err := s.queries.ListMailTemplatesByBook(ctx, bookID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list mail templates: %w", err))
	}

	out := make([]*mailtemplatev1.MailTemplate, 0, len(rows))
	for _, row := range rows {
		out = append(out, modelToProto(row))
	}

	return connect.NewResponse(&mailtemplatev1.ListMailTemplatesByBookResponse{
		Templates: out,
	}), nil
}
