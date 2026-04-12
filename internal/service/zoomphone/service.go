// Package zoomphone は Zoom Phone API を使った発信・録音取得の Connect サービス。
package zoomphone

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	zoomphonev1 "github.com/0utl1er-tech/phox-customer/gen/pb/zoomphone/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/0utl1er-tech/phox-customer/internal/zoom"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
)

type ZoomPhoneService struct {
	queries    *db.Queries
	zoomClient *zoom.Client
	authorizer *auth.Authorizer
}

func NewZoomPhoneService(queries *db.Queries, zoomClient *zoom.Client) *ZoomPhoneService {
	return &ZoomPhoneService{
		queries:    queries,
		zoomClient: zoomClient,
		authorizer: auth.NewAuthorizer(queries),
	}
}

// MakeCall は Zoom Phone API で発信し、同時に Activity (type=call) を記録する。
func (s *ZoomPhoneService) MakeCall(
	ctx context.Context,
	req *connect.Request[zoomphonev1.MakeCallRequest],
) (*connect.Response[zoomphonev1.MakeCallResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	// Keycloak email → Zoom Phone ユーザー解決
	var fromEmail string
	if email, ok := token.PrivateClaims()["email"].(string); ok {
		fromEmail = email
	}
	if fromEmail == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("Keycloak profile に email が設定されていません"))
	}

	if s.zoomClient == nil {
		return nil, connect.NewError(connect.CodeUnavailable,
			fmt.Errorf("Zoom Phone API が設定されていません"))
	}

	zoomUser, err := s.zoomClient.FindPhoneUserByEmail(fromEmail)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound,
			fmt.Errorf("Zoom Phone ユーザーが見つかりません: %s", fromEmail))
	}

	// 電話番号を E.164 に正規化
	calleeNumber := zoom.NormalizeJapanesePhone(req.Msg.PhoneNumber)

	// Zoom API で発信
	callInfo, err := s.zoomClient.MakeCall(zoomUser.ID, calleeNumber)
	if err != nil {
		log.Error().Err(err).Str("callee", calleeNumber).Msg("zoom phone: make call failed")
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("発信に失敗しました: %w", err))
	}

	// Activity (type=call) を記録 — customer_id が渡されていれば紐付け
	if req.Msg.CustomerId != "" {
		customerID, perr := uuid.Parse(req.Msg.CustomerId)
		if perr == nil {
			customer, gerr := s.queries.GetCustomer(ctx, customerID)
			if gerr == nil {
				// default status を取得 (Phase 20b で全 Book に seed 済)
				defaultStatus, serr := s.queries.GetDefaultStatusByBookID(ctx, customer.BookID)
				if serr == nil {
					_, aerr := s.queries.CreateActivity(ctx, db.CreateActivityParams{
						ID:         uuid.New(),
						CustomerID: customerID,
						Type:       "call",
						UserID:     userID,
						StatusID:   pgtype.UUID{Bytes: defaultStatus.ID, Valid: true},
						Phone:      pgtype.Text{String: req.Msg.PhoneNumber, Valid: true},
						OccurredAt: time.Now(),
					})
					if aerr != nil {
						log.Warn().Err(aerr).Msg("zoom phone: activity creation failed (call still went through)")
					}
				}
			}
		}
	}

	return connect.NewResponse(&zoomphonev1.MakeCallResponse{
		CallId: callInfo.CallID,
		Status: callInfo.Status,
	}), nil
}

// GetMyZoomPhoneStatus は現在ユーザーが Zoom Phone に紐付いているか返す。
func (s *ZoomPhoneService) GetMyZoomPhoneStatus(
	ctx context.Context,
	req *connect.Request[zoomphonev1.GetMyZoomPhoneStatusRequest],
) (*connect.Response[zoomphonev1.GetMyZoomPhoneStatusResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}

	var email string
	if e, ok := token.PrivateClaims()["email"].(string); ok {
		email = e
	}
	if email == "" || s.zoomClient == nil {
		return connect.NewResponse(&zoomphonev1.GetMyZoomPhoneStatusResponse{
			Connected: false,
		}), nil
	}

	zu, err := s.zoomClient.FindPhoneUserByEmail(email)
	if err != nil {
		return connect.NewResponse(&zoomphonev1.GetMyZoomPhoneStatusResponse{
			Connected: false,
		}), nil
	}

	return connect.NewResponse(&zoomphonev1.GetMyZoomPhoneStatusResponse{
		Connected:      true,
		ZoomPhoneNumber: zu.PhoneNumber,
		ZoomUserName:   zu.Name,
		Extension:      zu.ExtensionNumber,
	}), nil
}
