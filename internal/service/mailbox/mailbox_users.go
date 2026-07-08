package mailbox

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/google/uuid"
)

// AddMailboxUser grants a company user access to a mailbox. Owner only; the
// target must be in the same company; owner role cannot be granted here
// (mirrors permit.AddBookUser).
func (s *MailboxService) AddMailboxUser(
	ctx context.Context,
	req *connect.Request[mailboxv1.AddMailboxUserRequest],
) (*connect.Response[mailboxv1.AddMailboxUserResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	callerID := token.Subject()

	mailboxID, err := util.ParseUUID("mailbox_id", req.Msg.MailboxId)
	if err != nil {
		return nil, err
	}
	if err := s.authorizer.CheckMailboxPermission(ctx, mailboxID, db.RoleOwner); err != nil {
		return nil, err
	}

	callerUser, err := s.queries.GetUser(ctx, callerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get caller user: %w", err))
	}
	targetUser, err := s.queries.GetUser(ctx, req.Msg.UserId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("指定されたユーザーが見つかりません"))
	}
	if callerUser.CompanyID != targetUser.CompanyID {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("同じ会社のユーザーのみ追加できます"))
	}

	role := req.Msg.Role
	if role == permitv1.Role_ROLE_UNSPECIFIED {
		role = permitv1.Role_ROLE_VIEWER
	}
	if role == permitv1.Role_ROLE_OWNER {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("オーナー権限は付与できません"))
	}

	permit, err := s.queries.CreateMailboxPermit(ctx, db.CreateMailboxPermitParams{
		ID:        uuid.New(),
		MailboxID: mailboxID,
		UserID:    req.Msg.UserId,
		Role:      util.ConvertProtoRoleToDBRole(role),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("このユーザーは既にアクセス権を持っています"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create mailbox permit: %w", err))
	}

	return connect.NewResponse(&mailboxv1.AddMailboxUserResponse{
		User: &mailboxv1.MailboxUser{
			PermitId: permit.ID.String(),
			UserId:   permit.UserID,
			UserName: targetUser.Name,
			Role:     util.ConvertDBRoleToProtoRole(permit.Role),
		},
	}), nil
}

// ListMailboxUsers lists the mailbox members (viewer+ can read the roster).
func (s *MailboxService) ListMailboxUsers(
	ctx context.Context,
	req *connect.Request[mailboxv1.ListMailboxUsersRequest],
) (*connect.Response[mailboxv1.ListMailboxUsersResponse], error) {
	mailboxID, err := util.ParseUUID("mailbox_id", req.Msg.MailboxId)
	if err != nil {
		return nil, err
	}
	if err := s.authorizer.CheckMailboxPermission(ctx, mailboxID, db.RoleViewer); err != nil {
		return nil, err
	}

	rows, err := s.queries.ListMailboxPermitsWithUserInfo(ctx, mailboxID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list mailbox users: %w", err))
	}
	out := make([]*mailboxv1.MailboxUser, 0, len(rows))
	for _, r := range rows {
		out = append(out, permitRowToMailboxUser(r))
	}
	return connect.NewResponse(&mailboxv1.ListMailboxUsersResponse{Users: out}), nil
}

// UpdateMailboxUser changes a member's role. Owner only; cannot set owner.
func (s *MailboxService) UpdateMailboxUser(
	ctx context.Context,
	req *connect.Request[mailboxv1.UpdateMailboxUserRequest],
) (*connect.Response[mailboxv1.UpdateMailboxUserResponse], error) {
	mailboxID, err := util.ParseUUID("mailbox_id", req.Msg.MailboxId)
	if err != nil {
		return nil, err
	}
	if err := s.authorizer.CheckMailboxPermission(ctx, mailboxID, db.RoleOwner); err != nil {
		return nil, err
	}
	if req.Msg.Role == permitv1.Role_ROLE_OWNER {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("オーナー権限は付与できません"))
	}

	permit, err := s.queries.UpdateMailboxPermitRole(ctx, db.UpdateMailboxPermitRoleParams{
		MailboxID: mailboxID,
		UserID:    req.Msg.UserId,
		Role:      util.ConvertProtoRoleToDBRole(req.Msg.Role),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("該当するアクセス権が見つかりません"))
	}

	name := ""
	if u, uerr := s.queries.GetUser(ctx, permit.UserID); uerr == nil {
		name = u.Name
	}
	return connect.NewResponse(&mailboxv1.UpdateMailboxUserResponse{
		User: &mailboxv1.MailboxUser{
			PermitId: permit.ID.String(),
			UserId:   permit.UserID,
			UserName: name,
			Role:     util.ConvertDBRoleToProtoRole(permit.Role),
		},
	}), nil
}

// RemoveMailboxUser revokes a member's access. Owner only.
func (s *MailboxService) RemoveMailboxUser(
	ctx context.Context,
	req *connect.Request[mailboxv1.RemoveMailboxUserRequest],
) (*connect.Response[mailboxv1.RemoveMailboxUserResponse], error) {
	mailboxID, err := util.ParseUUID("mailbox_id", req.Msg.MailboxId)
	if err != nil {
		return nil, err
	}
	if err := s.authorizer.CheckMailboxPermission(ctx, mailboxID, db.RoleOwner); err != nil {
		return nil, err
	}
	if err := s.queries.DeleteMailboxPermit(ctx, db.DeleteMailboxPermitParams{
		MailboxID: mailboxID,
		UserID:    req.Msg.UserId,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to remove mailbox user: %w", err))
	}
	return connect.NewResponse(&mailboxv1.RemoveMailboxUserResponse{
		MailboxId: req.Msg.MailboxId,
		UserId:    req.Msg.UserId,
	}), nil
}
