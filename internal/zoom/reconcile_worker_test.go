package zoom

import (
	"testing"
	"time"
)

func TestShouldReconcile(t *testing.T) {
	tests := []struct {
		name      string
		hasClient bool
		mode      string
		want      bool
	}{
		{"zoom mode with client", true, CallLogModeZoom, true},
		{"click mode with client", true, CallLogModeClick, false},
		{"empty mode with client", true, "", false},
		{"zoom mode without client", false, CallLogModeZoom, false},
		{"click mode without client", false, CallLogModeClick, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := shouldReconcile(tt.hasClient, tt.mode)
			if got != tt.want {
				t.Errorf("shouldReconcile(%v, %q) = %v, want %v", tt.hasClient, tt.mode, got, tt.want)
			}
			if !got && reason == "" {
				t.Error("skip must come with a reason")
			}
			if got && reason != "" {
				t.Errorf("run decision must not carry a reason, got %q", reason)
			}
		})
	}
}

func TestReconcileWindow(t *testing.T) {
	now := time.Date(2026, 9, 3, 10, 30, 0, 0, time.UTC)
	from, to := reconcileWindow(now, 24*time.Hour)
	if from != "2026-09-02" {
		t.Errorf("from = %q, want 2026-09-02", from)
	}
	if to != "2026-09-03" {
		t.Errorf("to = %q, want 2026-09-03", to)
	}

	// 深夜帯 (lookback が日付を 2 日跨ぐケース)
	now = time.Date(2026, 9, 3, 0, 10, 0, 0, time.UTC)
	from, to = reconcileWindow(now, 24*time.Hour)
	if from != "2026-09-02" || to != "2026-09-03" {
		t.Errorf("window = %q..%q, want 2026-09-02..2026-09-03", from, to)
	}
}
