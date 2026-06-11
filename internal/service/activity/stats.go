package activity

import (
	"context"
	"fmt"
	"sort"

	"connectrpc.com/connect"
	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/util"
)

// GetCallStats は担当者 × コール結果 (Status) のクロス集計を返す。
// ロングフォーマット (1 セル = 1 要素) で返し、ピボットは UI 側で行う。
// 認可: 対象 Book に viewer 以上の権限が必要。
func (s *ActivityService) GetCallStats(
	ctx context.Context,
	req *connect.Request[activityv1.GetCallStatsRequest],
) (*connect.Response[activityv1.GetCallStatsResponse], error) {
	bookID, err := util.ParseUUID("book_id", req.Msg.BookId)
	if err != nil {
		return nil, err
	}

	if err := s.authorizer.CheckPermission(ctx, bookID, db.RoleViewer); err != nil {
		return nil, err
	}

	from, to := timeRange(req.Msg.OccurredFrom, req.Msg.OccurredTo)

	rows, err := s.queries.GetCallStatsByBook(ctx, db.GetCallStatsByBookParams{
		BookID:   bookID,
		FromTime: from,
		ToTime:   to,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get call stats: %w", err))
	}

	cells := make([]*activityv1.CallStatsCell, 0, len(rows))
	for _, r := range rows {
		cells = append(cells, &activityv1.CallStatsCell{
			UserId:               r.UserID,
			UserName:             r.UserName,
			StatusId:             uuidPtrString(r.StatusID),
			StatusName:           textPtr(r.StatusName),
			StatusPriority:       int32Ptr(r.StatusPriority),
			Count:                r.CallCount,
			TotalDurationSeconds: r.TotalDurationSeconds,
		})
	}

	return connect.NewResponse(&activityv1.GetCallStatsResponse{Cells: cells}), nil
}

// GetMailStats は担当者ごとのメール送信数と返信数を返す。
// 返信は「その顧客に最後に email_sent した担当者」への帰属で近似する
// (スレッド追跡は未実装)。帰属できない受信は user_id="" の行に集約。
// 認可: 対象 Book に viewer 以上の権限が必要。
func (s *ActivityService) GetMailStats(
	ctx context.Context,
	req *connect.Request[activityv1.GetMailStatsRequest],
) (*connect.Response[activityv1.GetMailStatsResponse], error) {
	bookID, err := util.ParseUUID("book_id", req.Msg.BookId)
	if err != nil {
		return nil, err
	}

	if err := s.authorizer.CheckPermission(ctx, bookID, db.RoleViewer); err != nil {
		return nil, err
	}

	from, to := timeRange(req.Msg.OccurredFrom, req.Msg.OccurredTo)

	sent, err := s.queries.GetMailSentStatsByBook(ctx, db.GetMailSentStatsByBookParams{
		BookID:   bookID,
		FromTime: from,
		ToTime:   to,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get mail sent stats: %w", err))
	}

	replies, err := s.queries.GetMailReplyStatsByBook(ctx, db.GetMailReplyStatsByBookParams{
		BookID:   bookID,
		FromTime: from,
		ToTime:   to,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get mail reply stats: %w", err))
	}

	// 送信と返信を user_id でマージ (どちらか一方にしか現れない担当者もいる)
	byUser := make(map[string]*activityv1.MailStatsRow)
	rowFor := func(userID, userName string) *activityv1.MailStatsRow {
		if row, ok := byUser[userID]; ok {
			return row
		}
		row := &activityv1.MailStatsRow{UserId: userID, UserName: userName}
		byUser[userID] = row
		return row
	}
	for _, r := range sent {
		rowFor(r.UserID, r.UserName).SentCount = r.SentCount
	}
	for _, r := range replies {
		rowFor(r.AttributedUserID, r.AttributedUserName).ReplyCount = r.ReplyCount
	}

	rows := make([]*activityv1.MailStatsRow, 0, len(byUser))
	for _, row := range byUser {
		rows = append(rows, row)
	}
	// map 順は不定なので表示安定のため名前順 (帰属不明 "" は末尾)
	sort.Slice(rows, func(i, j int) bool {
		if (rows[i].UserId == "") != (rows[j].UserId == "") {
			return rows[j].UserId == ""
		}
		return rows[i].UserName < rows[j].UserName
	})

	return connect.NewResponse(&activityv1.GetMailStatsResponse{Rows: rows}), nil
}
