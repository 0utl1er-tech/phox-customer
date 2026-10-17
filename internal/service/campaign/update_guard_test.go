package campaign

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

// validateStepEdit は DB を触らない純関数なので、ここで判定表を固定する。
// UI のロック表示 (max_sent_step) と同じ境界で拒否されることを保証したい。
func TestValidateStepEdit(t *testing.T) {
	const current = "ご挨拶"
	const subjectMsg = "送信済みの受信者がいるため、1通目の件名は変更できません（本文は変更できます）"
	const stepMsg3 = "3通目まで送信済みの受信者がいるため、3通目以前のステップは削除できません"

	cases := []struct {
		name          string
		newSubject    *string
		followupCount int
		progress      stepProgress
		wantMsg       string // 空 = nil を期待
	}{
		{
			name:          "未送信なら件名変更もステップ削除も通る",
			newSubject:    strPtr("別の件名"),
			followupCount: 1,
			progress:      stepProgress{SentCount: 0, MaxSentStep: 0},
		},
		{
			name:          "未送信なら max_sent_step が残っていても通る",
			newSubject:    strPtr("別の件名"),
			followupCount: 1,
			progress:      stepProgress{SentCount: 0, MaxSentStep: 3},
		},
		{
			name:       "送信済みでも件名が同値なら通る",
			newSubject: strPtr(current),
			progress:   stepProgress{SentCount: 5, MaxSentStep: 1},
		},
		{
			name:     "送信済みでも件名未指定なら通る",
			progress: stepProgress{SentCount: 5, MaxSentStep: 1},
		},
		{
			name:       "送信済みで件名変更は拒否",
			newSubject: strPtr("別の件名"),
			progress:   stepProgress{SentCount: 1, MaxSentStep: 1},
			wantMsg:    subjectMsg,
		},
		{
			name:          "max=3 で 2 通構成 (followups 1 件) は拒否",
			followupCount: 1,
			progress:      stepProgress{SentCount: 4, MaxSentStep: 3},
			wantMsg:       stepMsg3,
		},
		{
			name:          "max=3 で 3 通構成 (followups 2 件) は通る",
			followupCount: 2,
			progress:      stepProgress{SentCount: 4, MaxSentStep: 3},
		},
		{
			name:          "max=3 で 4 通目削除 (5 通 → 4 通構成) は通る",
			followupCount: 3,
			progress:      stepProgress{SentCount: 4, MaxSentStep: 3},
		},
		{
			name:          "followups 未指定 (変更なし) はステップガード対象外",
			followupCount: 0,
			progress:      stepProgress{SentCount: 4, MaxSentStep: 3},
		},
		{
			name:          "件名変更とステップ削除が同時なら件名エラーを優先",
			newSubject:    strPtr("別の件名"),
			followupCount: 1,
			progress:      stepProgress{SentCount: 4, MaxSentStep: 3},
			wantMsg:       subjectMsg,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStepEdit(current, tc.newSubject, tc.followupCount, tc.progress)
			if tc.wantMsg == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			var cerr *connect.Error
			require.True(t, errors.As(err, &cerr), "connect.Error であること: %v", err)
			assert.Equal(t, connect.CodeFailedPrecondition, cerr.Code())
			assert.Contains(t, cerr.Message(), tc.wantMsg)
		})
	}
}
