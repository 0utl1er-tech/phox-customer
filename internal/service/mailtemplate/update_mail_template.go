package mailtemplate

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mailtemplatev1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailtemplate/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
)

// UpdateMailTemplate は既存テンプレを更新する。
// 認可: テンプレの属する Book に editor 以上の権限が必要。
func (s *MailTemplateService) UpdateMailTemplate(
	ctx context.Context,
	req *connect.Request[mailtemplatev1.UpdateMailTemplateRequest],
) (*connect.Response[mailtemplatev1.UpdateMailTemplateResponse], error) {
	id, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid id: %w", err))
	}

	existing, err := s.queries.GetMailTemplate(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("mail template not found: %w", err))
	}

	if err := s.authorizer.CheckPermission(ctx, existing.BookID, db.RoleEditor); err != nil {
		return nil, err
	}

	updated, err := s.queries.UpdateMailTemplate(ctx, db.UpdateMailTemplateParams{
		ID:      id,
		Name:    req.Msg.Name,
		Subject: req.Msg.Subject,
		Body:    req.Msg.Body,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update mail template: %w", err))
	}

	return connect.NewResponse(&mailtemplatev1.UpdateMailTemplateResponse{
		Template: modelToProto(updated),
	}), nil
}
