package activity

import (
	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// typeStringToProto は DB の type 文字列を proto enum に変換する。
func typeStringToProto(s string) activityv1.ActivityType {
	switch s {
	case "call":
		return activityv1.ActivityType_ACTIVITY_TYPE_CALL
	case "email_sent":
		return activityv1.ActivityType_ACTIVITY_TYPE_EMAIL_SENT
	case "email_received":
		return activityv1.ActivityType_ACTIVITY_TYPE_EMAIL_RECEIVED
	default:
		return activityv1.ActivityType_ACTIVITY_TYPE_UNSPECIFIED
	}
}

func ptrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid || t.String == "" {
		return nil
	}
	v := t.String
	return &v
}

func uuidPtrString(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	// pgtype.UUID.Bytes は [16]byte。uuid.UUID と同じ表現。
	var bytes [16]byte = u.Bytes
	// RFC 4122 hex 形式に整形する
	const hex = "0123456789abcdef"
	buf := make([]byte, 36)
	pos := 0
	for i, b := range bytes {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			buf[pos] = '-'
			pos++
		}
		buf[pos] = hex[b>>4]
		buf[pos+1] = hex[b&0x0f]
		pos += 2
	}
	s := string(buf)
	return &s
}

func int32Ptr(n pgtype.Int4) *int32 {
	if !n.Valid {
		return nil
	}
	v := n.Int32
	return &v
}

func boolPtr(b pgtype.Bool) *bool {
	if !b.Valid {
		return nil
	}
	v := b.Bool
	return &v
}

// rowToProto は `ListActivitiesByCustomerID` の JOIN 済み行を proto Activity に変換。
//
// recording_url 自体は `s3://...` 形式の internal path なので UI へは渡さず、
// 「録音があるか」のフラグだけ has_recording として露出する。再生時は別 RPC
// `GetActivityRecording` で短命 presigned URL を発行する流れ。
func rowToProto(r db.ListActivitiesByCustomerIDRow) *activityv1.Activity {
	return &activityv1.Activity{
		Id:              r.ID.String(),
		CustomerId:      r.CustomerID.String(),
		ContactId:       uuidPtrString(r.ContactID),
		Type:            typeStringToProto(r.Type),
		UserId:          r.UserID,
		UserName:        r.UserName,
		StatusId:        uuidPtrString(r.StatusID),
		StatusName:      textPtr(r.StatusName),
		StatusPriority:  int32Ptr(r.StatusPriority),
		StatusEffective: boolPtr(r.StatusEffective),
		StatusNg:        boolPtr(r.StatusNg),
		Phone:           textPtr(r.Phone),
		MailFrom:        textPtr(r.MailFrom),
		MailTo:          textPtr(r.MailTo),
		MailCc:          textPtr(r.MailCc),
		Subject:         textPtr(r.Subject),
		Body:            textPtr(r.Body),
		MessageId:       textPtr(r.MessageID),
		HasRecording:    r.RecordingUrl.Valid && r.RecordingUrl.String != "",
		DurationSeconds: int32Ptr(r.DurationSeconds),
		OccurredAt:      timestamppb.New(r.OccurredAt),
		CreatedAt:       timestamppb.New(r.CreatedAt),
	}
}

// modelToProto は Activity モデル (Create/Update の戻り値) を proto Activity に変換。
// JOIN が無いので user_name/status_* はクライアント側で別途 lookup するか空のまま返す。
func modelToProto(a db.Activity) *activityv1.Activity {
	return &activityv1.Activity{
		Id:         a.ID.String(),
		CustomerId: a.CustomerID.String(),
		ContactId:  uuidPtrString(a.ContactID),
		Type:       typeStringToProto(a.Type),
		UserId:     a.UserID,
		StatusId:   uuidPtrString(a.StatusID),
		Phone:      textPtr(a.Phone),
		MailFrom:   textPtr(a.MailFrom),
		MailTo:     textPtr(a.MailTo),
		MailCc:     textPtr(a.MailCc),
		Subject:    textPtr(a.Subject),
		Body:       textPtr(a.Body),
		MessageId:  textPtr(a.MessageID),
		OccurredAt: timestamppb.New(a.OccurredAt),
		CreatedAt:  timestamppb.New(a.CreatedAt),
	}
}
