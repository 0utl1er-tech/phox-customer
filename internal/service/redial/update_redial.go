package redial

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	redialv1 "github.com/0utl1er-tech/phox-customer/gen/pb/redial/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/gcal"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

func (s *RedialService) UpdateRedial(
	ctx context.Context,
	req *connect.Request[redialv1.UpdateRedialRequest],
) (*connect.Response[redialv1.UpdateRedialResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	id, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid id: %w", err))
	}
	if req.Msg.StartAt == nil || req.Msg.EndAt == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("start_at and end_at are required"))
	}

	existing, err := s.queries.GetRedial(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("redial not found: %w", err))
	}
	customer, err := s.queries.GetCustomer(ctx, existing.CustomerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("customer not found: %w", err))
	}
	if err := s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleEditor); err != nil {
		return nil, err
	}

	row, err := s.queries.UpdateRedial(ctx, db.UpdateRedialParams{
		ID:      id,
		Phone:   req.Msg.Phone,
		StartAt: req.Msg.StartAt.AsTime(),
		EndAt:   req.Msg.EndAt.AsTime(),
		Note:    req.Msg.Note,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update redial: %w", err))
	}

	// GCal patch (failure tolerated)
	hasToken := s.lookupUserHasGoogle(ctx, userID)
	syncStatus := redialv1.SyncStatus_SYNC_STATUS_NOT_CONNECTED
	if hasToken {
		syncStatus = redialv1.SyncStatus_SYNC_STATUS_UNSYNCED
		if s.gcalClient != nil && row.GcalEventID.Valid && row.GcalEventID.String != "" {
			phoxBase := ""
			if v, ok := ctx.Value(ctxKeyPhoxBaseURL{}).(string); ok {
				phoxBase = v
			}
			input := gcalEventFromRedial(row, customer.Name, customer.BookID.String(), phoxBase)
			if perr := s.gcalClient.PatchEvent(ctx, userID, row.GcalEventID.String, input); perr != nil {
				if errors.Is(perr, gcal.ErrTokenRevoked) {
					log.Warn().Str("user_id", userID).Msg("redial: google token revoked — update kept unsynced")
					syncStatus = redialv1.SyncStatus_SYNC_STATUS_NOT_CONNECTED
				} else {
					log.Warn().Err(perr).Str("redial_id", row.ID.String()).Msg("redial: gcal patch failed")
				}
			} else {
				syncStatus = redialv1.SyncStatus_SYNC_STATUS_SYNCED
			}
		} else if row.GcalEventID.Valid && row.GcalEventID.String != "" {
			syncStatus = redialv1.SyncStatus_SYNC_STATUS_SYNCED
		}
	}

	userName := row.UserID
	if u, uerr := s.queries.GetUser(ctx, row.UserID); uerr == nil {
		userName = u.Name
	}

	return connect.NewResponse(&redialv1.UpdateRedialResponse{
		Redial: modelToProto(row, userName, syncStatus),
	}), nil
}
