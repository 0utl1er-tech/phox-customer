package campaign

import (
	"fmt"

	"connectrpc.com/connect"
)

// stepProgress — 編集ガードの判定に使う送信進捗。GetCampaignStats の値を
// そのまま詰める (ガード専用のクエリは増やさない)。
type stepProgress struct {
	SentCount   int64 // 1 通以上送った受信者数 (stats.sent)
	MaxSentStep int32 // stats.max_sent_step
}

// validateStepEdit は、送信実績のあるキャンペーンの文面編集を検査する。
//   - 1 件以上送信済みで subject を現在値と異なる値に変えようとした → FailedPrecondition
//     (受け取った人と未送信の人で 1 通目の件名が割れ、返信スレッドも追えなくなる)
//   - followups を指定し、len(followups)+1 < MaxSentStep → FailedPrecondition
//     (= 誰かが受け取ったステップを消そうとしている)
//
// どちらにも当たらなければ nil。SentCount == 0 なら何もしない
// (draft の自由編集を壊さない)。followupCount == 0 は API 仕様で
// 「フォローアップ変更なし」なのでステップガードの対象外。
func validateStepEdit(currentSubject string, newSubject *string, followupCount int, p stepProgress) error {
	if p.SentCount == 0 {
		return nil
	}
	if newSubject != nil && *newSubject != currentSubject {
		return connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("送信済みの受信者がいるため、1通目の件名は変更できません（本文は変更できます）"))
	}
	if followupCount > 0 && int32(followupCount+1) < p.MaxSentStep {
		return connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("%d通目まで送信済みの受信者がいるため、%d通目以前のステップは削除できません",
				p.MaxSentStep, p.MaxSentStep))
	}
	return nil
}
