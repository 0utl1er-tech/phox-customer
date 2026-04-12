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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
)

// CreateRedial は新しい掛け直し予定を作成する。
// 認可: Book に editor 以上の権限が必要。
// 実装方針: DB insert を先行 → GCal insert を後段で試行 (失敗しても Redial は残す)。
// 失敗時は gcal_event_id NULL のまま返し、UI に "unsynced" バッジを出す。
func (s *RedialService) CreateRedial(
	ctx context.Context,
	req *connect.Request[redialv1.CreateRedialRequest],
) (*connect.Response[redialv1.CreateRedialResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	customerID, err := uuid.Parse(req.Msg.CustomerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid customer_id: %w", err))
	}
	if req.Msg.StartAt == nil || req.Msg.EndAt == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("start_at and end_at are required"))
	}

	customer, err := s.queries.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("customer not found: %w", err))
	}
	if err := s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleEditor); err != nil {
		return nil, err
	}

	row, err := s.queries.CreateRedial(ctx, db.CreateRedialParams{
		ID:           uuid.New(),
		CustomerID:   customerID,
		UserID:       userID,
		Phone:        req.Msg.Phone,
		StartAt:      req.Msg.StartAt.AsTime(),
		EndAt:        req.Msg.EndAt.AsTime(),
		Note:         req.Msg.Note,
		GcalEventID:  pgtype.Text{Valid: false},
		GcalSyncedAt: pgtype.Timestamptz{Valid: false},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create redial: %w", err))
	}

	// GCal 同期 (副作用後段、失敗しても DB は残す)
	hasToken := s.lookupUserHasGoogle(ctx, userID)
	syncStatus := redialv1.SyncStatus_SYNC_STATUS_NOT_CONNECTED
	if hasToken && s.gcalClient != nil {
		phoxBase := ""
		if cfg, ok := ctx.Value(ctxKeyPhoxBaseURL{}).(string); ok {
			phoxBase = cfg
		}
		input := gcalEventFromRedial(row, customer.Name, customer.BookID.String(), phoxBase)
		eventID, gerr := s.gcalClient.CreateEvent(ctx, userID, input)
		if gerr == nil && eventID != "" {
			row, err = s.queries.SetRedialGcalSynced(ctx, db.SetRedialGcalSyncedParams{
				ID:          row.ID,
				GcalEventID: pgtype.Text{String: eventID, Valid: true},
			})
			if err != nil {
				log.Warn().Err(err).Str("redial_id", row.ID.String()).Msg("redial: failed to set gcal_event_id")
				syncStatus = redialv1.SyncStatus_SYNC_STATUS_UNSYNCED
			} else {
				syncStatus = redialv1.SyncStatus_SYNC_STATUS_SYNCED
			}
		} else {
			// ErrTokenRevoked の場合、real.go が UserGoogleToken を削除済みなので
			// UI 側で「未連携」として扱わせる。そうでなければ一時的失敗で unsynced。
			if errors.Is(gerr, gcal.ErrTokenRevoked) {
				log.Warn().Str("user_id", userID).Msg("redial: google token revoked — row stored unsynced")
				syncStatus = redialv1.SyncStatus_SYNC_STATUS_NOT_CONNECTED
			} else {
				log.Warn().Err(gerr).Str("user_id", userID).Msg("redial: gcal create failed — keeping unsynced")
				syncStatus = redialv1.SyncStatus_SYNC_STATUS_UNSYNCED
			}
		}
	}

	userName := userID
	if u, uerr := s.queries.GetUser(ctx, userID); uerr == nil {
		userName = u.Name
	}

	return connect.NewResponse(&redialv1.CreateRedialResponse{
		Redial: modelToProto(row, userName, syncStatus),
	}), nil
}

// Phase 20.3 context key for phox base URL — main.go injects it in the request context
// via an http middleware so gcal event descriptions can include deep links.
// (unused if middleware not wired — safe default "".)
type ctxKeyPhoxBaseURL struct{}
