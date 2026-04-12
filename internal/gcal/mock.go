package gcal

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MockCall は Mock が受けた 1 回の呼び出しを記録する。
// E2E テストで /debug/gcal/calls から JSON で取得される。
type MockCall struct {
	Op      string    `json:"op"`       // create | patch | delete
	UserID  string    `json:"user_id"`
	EventID string    `json:"event_id"` // create の場合は採番された ID
	At      time.Time `json:"at"`

	// 以下 create / patch で使われるペイロード。
	Summary     string    `json:"summary,omitempty"`
	Description string    `json:"description,omitempty"`
	StartAt     time.Time `json:"start_at,omitempty"`
	EndAt       time.Time `json:"end_at,omitempty"`
}

// MockClient は GCAL_MODE=mock のときに Client として差し込まれる。
// 呼び出しを in-memory に蓄積し、debug endpoint でダンプできる。
type MockClient struct {
	mu    sync.Mutex
	calls []MockCall
	// 存在する event_id set (DeleteEvent 後は false)
	events map[string]bool
}

func NewMockClient() *MockClient {
	return &MockClient{
		events: make(map[string]bool),
	}
}

func (m *MockClient) record(c MockCall) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.At = time.Now().UTC()
	m.calls = append(m.calls, c)
}

func (m *MockClient) CreateEvent(
	ctx context.Context,
	userID string,
	in EventInput,
) (string, error) {
	eventID := fmt.Sprintf("mock-evt-%s", uuid.NewString())
	m.mu.Lock()
	m.events[eventID] = true
	m.mu.Unlock()
	m.record(MockCall{
		Op:          "create",
		UserID:      userID,
		EventID:     eventID,
		Summary:     in.Summary,
		Description: in.Description,
		StartAt:     in.StartAt,
		EndAt:       in.EndAt,
	})
	return eventID, nil
}

func (m *MockClient) PatchEvent(
	ctx context.Context,
	userID, eventID string,
	in EventInput,
) error {
	m.record(MockCall{
		Op:          "patch",
		UserID:      userID,
		EventID:     eventID,
		Summary:     in.Summary,
		Description: in.Description,
		StartAt:     in.StartAt,
		EndAt:       in.EndAt,
	})
	return nil
}

func (m *MockClient) DeleteEvent(
	ctx context.Context,
	userID, eventID string,
) error {
	m.mu.Lock()
	delete(m.events, eventID)
	m.mu.Unlock()
	m.record(MockCall{
		Op:      "delete",
		UserID:  userID,
		EventID: eventID,
	})
	return nil
}

// Calls は記録された呼び出しのコピーを返す (テスト検証用)。
func (m *MockClient) Calls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.calls))
	copy(out, m.calls)
	return out
}

// Reset は in-memory 状態をクリアする (テスト間の isolation 用)。
func (m *MockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = nil
	m.events = make(map[string]bool)
}
