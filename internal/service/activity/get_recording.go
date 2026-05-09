package activity

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/recording"
)

// GetActivityRecording は activity の通話録音を再生するための短命 signed URL
// を発行する。実体は recording.Service.IssueSignedURL に委譲し、こちらは
// 認可 (activity が指す customer の book に viewer 以上の permit) のみ担当。
func (s *ActivityService) GetActivityRecording(
	ctx context.Context,
	req *connect.Request[activityv1.GetActivityRecordingRequest],
) (*connect.Response[activityv1.GetActivityRecordingResponse], error) {
	if s.recordingSvc == nil || !s.recordingSvc.IsEnabled() {
		return nil, connect.NewError(connect.CodeUnavailable,
			fmt.Errorf("recording playback is not configured"))
	}

	id, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid id: %w", err))
	}

	// permit 確認のために activity → customer → book を引く。
	a, err := s.queries.GetActivity(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("activity not found: %w", err))
	}
	customer, err := s.queries.GetCustomer(ctx, a.CustomerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("customer not found: %w", err))
	}
	if err := s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleViewer); err != nil {
		return nil, err
	}

	url, exp, err := s.recordingSvc.IssueSignedURL(ctx, id)
	switch {
	case errors.Is(err, recording.ErrNoRecording):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, recording.ErrActivityNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, recording.ErrDisabled):
		return nil, connect.NewError(connect.CodeUnavailable, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&activityv1.GetActivityRecordingResponse{
		Url:       url,
		ExpiresAt: timestamppb.New(exp),
	}), nil
}
