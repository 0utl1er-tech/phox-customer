package activity

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// timeRange は optional な proto Timestamp ペアを SQL に渡せる閉区間に展開する。
// 未指定側は epoch / 遠未来のセンチネルで「無制限」を表す (クエリ側の
// from <= occurred_at < to に対応)。
func timeRange(from, to *timestamppb.Timestamp) (time.Time, time.Time) {
	f := time.Unix(0, 0)
	if from != nil {
		f = from.AsTime()
	}
	t := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	if to != nil {
		t = to.AsTime()
	}
	return f, t
}

// typesToStrings は proto enum の配列を DB の type 文字列配列に変換する。
// UNSPECIFIED は無視される (空配列 = 全 type)。
func typesToStrings(types []activityv1.ActivityType) []string {
	out := make([]string, 0, len(types))
	for _, t := range types {
		switch t {
		case activityv1.ActivityType_ACTIVITY_TYPE_CALL:
			out = append(out, "call")
		case activityv1.ActivityType_ACTIVITY_TYPE_EMAIL_SENT:
			out = append(out, "email_sent")
		case activityv1.ActivityType_ACTIVITY_TYPE_EMAIL_RECEIVED:
			out = append(out, "email_received")
		}
	}
	return out
}

// bookRowToProto は ListActivitiesByBookID の JOIN 済み行を proto Activity に変換。
// rowToProto との違いは customer_name / customer_corporation が埋まること。
func bookRowToProto(r db.ListActivitiesByBookIDRow) *activityv1.Activity {
	return &activityv1.Activity{
		Id:                  r.ID.String(),
		CustomerId:          r.CustomerID.String(),
		ContactId:           uuidPtrString(r.ContactID),
		Type:                typeStringToProto(r.Type),
		UserId:              r.UserID,
		UserName:            r.UserName,
		StatusId:            uuidPtrString(r.StatusID),
		StatusName:          textPtr(r.StatusName),
		StatusPriority:      int32Ptr(r.StatusPriority),
		StatusEffective:     boolPtr(r.StatusEffective),
		StatusNg:            boolPtr(r.StatusNg),
		Phone:               textPtr(r.Phone),
		MailFrom:            textPtr(r.MailFrom),
		MailTo:              textPtr(r.MailTo),
		MailCc:              textPtr(r.MailCc),
		Subject:             textPtr(r.Subject),
		Body:                textPtr(r.Body),
		MessageId:           textPtr(r.MessageID),
		HasRecording:        r.RecordingUrl.Valid && r.RecordingUrl.String != "",
		DurationSeconds:     int32Ptr(r.DurationSeconds),
		OccurredAt:          timestamppb.New(r.OccurredAt),
		CreatedAt:           timestamppb.New(r.CreatedAt),
		CustomerName:        ptrIfNotEmpty(r.CustomerName),
		CustomerCorporation: ptrIfNotEmpty(r.CustomerCorporation),
	}
}

// ListActivitiesByBookID は Book 内の全顧客の Activity を横断で返す活動フィード。
// 種別 / 担当者 / 期間でフィルタでき、occurred_at 降順 + offset ページング。
// 認可: 対象 Book に viewer 以上の権限が必要。
func (s *ActivityService) ListActivitiesByBookID(
	ctx context.Context,
	req *connect.Request[activityv1.ListActivitiesByBookIDRequest],
) (*connect.Response[activityv1.ListActivitiesByBookIDResponse], error) {
	bookID, err := util.ParseUUID("book_id", req.Msg.BookId)
	if err != nil {
		return nil, err
	}

	if err := s.authorizer.CheckPermission(ctx, bookID, db.RoleViewer); err != nil {
		return nil, err
	}

	limit := req.Msg.Limit
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	offset := req.Msg.Offset
	if offset < 0 {
		offset = 0
	}

	from, to := timeRange(req.Msg.OccurredFrom, req.Msg.OccurredTo)
	filterUserID := ""
	if req.Msg.UserId != nil {
		filterUserID = *req.Msg.UserId
	}
	typeStrs := typesToStrings(req.Msg.Types)

	rows, err := s.queries.ListActivitiesByBookID(ctx, db.ListActivitiesByBookIDParams{
		BookID:       bookID,
		Limit:        limit,
		Offset:       offset,
		Types:        typeStrs,
		FilterUserID: filterUserID,
		FromTime:     from,
		ToTime:       to,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list activities by book: %w", err))
	}

	total, err := s.queries.CountActivitiesByBookID(ctx, db.CountActivitiesByBookIDParams{
		BookID:       bookID,
		Types:        typeStrs,
		FilterUserID: filterUserID,
		FromTime:     from,
		ToTime:       to,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("count activities by book: %w", err))
	}

	activities := make([]*activityv1.Activity, 0, len(rows))
	for _, r := range rows {
		activities = append(activities, bookRowToProto(r))
	}

	return connect.NewResponse(&activityv1.ListActivitiesByBookIDResponse{
		Activities: activities,
		TotalCount: total,
	}), nil
}
