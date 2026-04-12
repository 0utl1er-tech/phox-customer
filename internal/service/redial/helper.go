package redial

import (
	"context"
	"errors"
	"fmt"

	redialv1 "github.com/0utl1er-tech/phox-customer/gen/pb/redial/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/gcal"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// modelToProto converts a Redial row to its proto representation.
// userName は呼び出し側 (ListRedialsByCustomer で JOIN 結果) が詰める想定だが、
// 単独クエリ経由ではこの関数が User を補完しない — 呼び出し側の責任。
func modelToProto(r db.Redial, userName string, status redialv1.SyncStatus) *redialv1.Redial {
	out := &redialv1.Redial{
		Id:         r.ID.String(),
		CustomerId: r.CustomerID.String(),
		UserId:     r.UserID,
		UserName:   userName,
		Phone:      r.Phone,
		StartAt:    timestamppb.New(r.StartAt),
		EndAt:      timestamppb.New(r.EndAt),
		Note:       r.Note,
		CreatedAt:  timestamppb.New(r.CreatedAt),
		UpdatedAt:  timestamppb.New(r.UpdatedAt),
		SyncStatus: status,
	}
	if r.GcalEventID.Valid && r.GcalEventID.String != "" {
		v := r.GcalEventID.String
		out.GcalEventId = &v
	}
	if r.GcalSyncedAt.Valid {
		out.GcalSyncedAt = timestamppb.New(r.GcalSyncedAt.Time)
	}
	return out
}

// deriveSyncStatus は row の gcal_event_id とユーザーの Google 連携状況から
// 3 値のステータスを計算する。hasGoogleToken は呼び出し前に解決済みの bool。
func deriveSyncStatus(row db.Redial, hasGoogleToken bool) redialv1.SyncStatus {
	if !hasGoogleToken {
		return redialv1.SyncStatus_SYNC_STATUS_NOT_CONNECTED
	}
	if row.GcalEventID.Valid && row.GcalEventID.String != "" {
		return redialv1.SyncStatus_SYNC_STATUS_SYNCED
	}
	return redialv1.SyncStatus_SYNC_STATUS_UNSYNCED
}

// lookupUserHasGoogle は UserGoogleToken の存在有無を返す。エラー時は false。
func (s *RedialService) lookupUserHasGoogle(ctx context.Context, userID string) bool {
	_, err := s.queries.GetUserGoogleToken(ctx, userID)
	return err == nil
}

// gcalEventFromRedial は Redial 行 + Customer 情報から GCal イベント入力を組み立てる。
func gcalEventFromRedial(r db.Redial, customerName, bookID, phoxBaseURL string) gcal.EventInput {
	summary := fmt.Sprintf("[Phox] %s へ掛け直し", customerName)
	description := r.Note
	if r.Phone != "" {
		if description != "" {
			description += "\n"
		}
		description += "電話: " + r.Phone
	}
	if phoxBaseURL != "" {
		if description != "" {
			description += "\n"
		}
		description += fmt.Sprintf("%s/book/%s/customer/%s", phoxBaseURL, bookID, r.CustomerID.String())
	}
	return gcal.EventInput{
		Summary:     summary,
		Description: description,
		StartAt:     r.StartAt,
		EndAt:       r.EndAt,
		TimeZone:    "Asia/Tokyo",
	}
}

// ErrGcalUnavailable is returned when gcalClient is nil.
var ErrGcalUnavailable = errors.New("redial: gcal client not configured")
