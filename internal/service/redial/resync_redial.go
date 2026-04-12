package redial

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	redialv1 "github.com/0utl1er-tech/phox-customer/gen/pb/redial/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ResyncRedial は gcal_event_id が NULL の Redial を再試行で GCal に作成する。
// 既に gcal_event_id を持っている行に対しては no-op (status だけ返す)。
func (s *RedialService) ResyncRedial(
	ctx context.Context,
	req *connect.Request[redialv1.ResyncRedialRequest],
) (*connect.Response[redialv1.ResyncRedialResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	id, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid id: %w", err))
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

	hasToken := s.lookupUserHasGoogle(ctx, userID)
	if !hasToken {
		// 未連携ならフロントでカバーされるべきだが、念のため明示エラー。
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("google account not connected"))
	}
	if s.gcalClient == nil {
		return nil, connect.NewError(connect.CodeUnavailable, ErrGcalUnavailable)
	}

	row := existing
	status := redialv1.SyncStatus_SYNC_STATUS_SYNCED

	if !(row.GcalEventID.Valid && row.GcalEventID.String != "") {
		phoxBase := ""
		if v, ok := ctx.Value(ctxKeyPhoxBaseURL{}).(string); ok {
			phoxBase = v
		}
		input := gcalEventFromRedial(row, customer.Name, customer.BookID.String(), phoxBase)
		eventID, gerr := s.gcalClient.CreateEvent(ctx, userID, input)
		if gerr != nil {
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("gcal create failed: %w", gerr))
		}
		row, err = s.queries.SetRedialGcalSynced(ctx, db.SetRedialGcalSyncedParams{
			ID:          row.ID,
			GcalEventID: pgtype.Text{String: eventID, Valid: true},
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("set gcal synced: %w", err))
		}
	}

	userName := row.UserID
	if u, uerr := s.queries.GetUser(ctx, row.UserID); uerr == nil {
		userName = u.Name
	}

	return connect.NewResponse(&redialv1.ResyncRedialResponse{
		Redial: modelToProto(row, userName, status),
	}), nil
}
