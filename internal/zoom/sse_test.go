package zoom_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0utl1er-tech/phox-customer/internal/zoom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEHub_SubscribeAndBroadcast(t *testing.T) {
	hub := zoom.NewSSEHub()

	ch := hub.Subscribe("joe@test.com")
	defer hub.Unsubscribe("joe@test.com", ch)

	n := zoom.CallNotification{
		Type:         "ringing",
		CallID:       "call-1",
		CallerNumber: "03-1234-5678",
		CallerName:   "田中太郎",
		Direction:    "inbound",
	}
	hub.Broadcast("joe@test.com", n)

	select {
	case got := <-ch:
		assert.Equal(t, "ringing", got.Type)
		assert.Equal(t, "call-1", got.CallID)
		assert.Equal(t, "田中太郎", got.CallerName)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for broadcast")
	}
}

func TestSSEHub_BroadcastAll(t *testing.T) {
	hub := zoom.NewSSEHub()

	ch1 := hub.Subscribe("user1@test.com")
	ch2 := hub.Subscribe("user2@test.com")
	defer hub.Unsubscribe("user1@test.com", ch1)
	defer hub.Unsubscribe("user2@test.com", ch2)

	hub.Broadcast("", zoom.CallNotification{Type: "ringing", CallID: "global"})

	for _, ch := range []chan zoom.CallNotification{ch1, ch2} {
		select {
		case got := <-ch:
			assert.Equal(t, "global", got.CallID)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	}
}

func TestSSEHub_UnsubscribePreventsReceive(t *testing.T) {
	hub := zoom.NewSSEHub()
	ch := hub.Subscribe("joe@test.com")
	hub.Unsubscribe("joe@test.com", ch)

	hub.Broadcast("joe@test.com", zoom.CallNotification{Type: "ringing"})
	// ch is closed, so receive should return zero value immediately
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed")
	default:
		// ok — channel was drained or closed
	}
}

func TestSSEHub_ServeHTTP_RequiresEmail(t *testing.T) {
	hub := zoom.NewSSEHub()
	req := httptest.NewRequest("GET", "/sse/calls", nil) // no email param
	rec := httptest.NewRecorder()
	hub.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// 回帰: クライアント切断で SSE ハンドラが抜け、Unsubscribe されること。
// 以前は r.Context().Done() だけに依存しており、プロキシ経由で Done が
// 発火しないと goroutine と購読エントリがリークしていた (2026-07-02)。
func TestSSEHub_ServeHTTP_UnsubscribesOnClientDisconnect(t *testing.T) {
	hub := zoom.NewSSEHub()
	srv := httptest.NewServer(hub)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/sse/calls?email=disc@test.com", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	// 初期 ping が届く = 購読確立。
	buf := make([]byte, 64)
	_, _ = resp.Body.Read(buf)
	require.Eventually(t, func() bool { return hub.ClientCount() == 1 }, time.Second, 5*time.Millisecond,
		"client should be subscribed")

	// クライアント切断 (ブラウザが去るのに相当)。
	cancel()
	_ = resp.Body.Close()

	// ハンドラが抜けて Unsubscribe され、購読が 0 に戻る。
	require.Eventually(t, func() bool { return hub.ClientCount() == 0 }, 2*time.Second, 10*time.Millisecond,
		"handler must unsubscribe after client disconnects")
}

// heartbeat の書き込み失敗でも確実に抜けることを、短い interval で検証。
func TestSSEHub_ServeHTTP_HeartbeatDetectsDeadClient(t *testing.T) {
	restore := zoom.SetSSEHeartbeatIntervalForTest(20 * time.Millisecond)
	defer restore()

	hub := zoom.NewSSEHub()
	srv := httptest.NewServer(hub)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/sse/calls?email=hb@test.com")
	require.NoError(t, err)
	buf := make([]byte, 64)
	_, _ = resp.Body.Read(buf)
	require.Eventually(t, func() bool { return hub.ClientCount() == 1 }, time.Second, 5*time.Millisecond)

	// TCP を即クローズ。次の heartbeat 書き込みが失敗してハンドラが抜ける。
	_ = resp.Body.Close()
	require.Eventually(t, func() bool { return hub.ClientCount() == 0 }, 2*time.Second, 10*time.Millisecond,
		"heartbeat write failure should end the handler")
}

// 上限寿命に達したら自発的に閉じることを検証。
func TestSSEHub_ServeHTTP_MaxLifetime(t *testing.T) {
	restore := zoom.SetSSEMaxLifetimeForTest(80 * time.Millisecond)
	defer restore()

	hub := zoom.NewSSEHub()
	srv := httptest.NewServer(hub)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/sse/calls?email=life@test.com")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Eventually(t, func() bool { return hub.ClientCount() == 1 }, time.Second, 5*time.Millisecond)

	// クライアントは切断していないが、maxLifetime で server 側から閉じる。
	require.Eventually(t, func() bool { return hub.ClientCount() == 0 }, 2*time.Second, 10*time.Millisecond,
		"handler must close itself at max lifetime")
}

func TestWebhookHandler_PhoneEnded(t *testing.T) {
	handler := zoom.NewWebhookHandler("")
	var ended *zoom.PhoneCallEvent
	handler.OnCallEnded(func(ev zoom.PhoneCallEvent) {
		ended = &ev
	})

	// phone.caller_ended は staff 側が発信 → direction=outbound に導出される。
	// 時刻 5 秒差 → DurationSec=5。
	body := `{
		"event": "phone.caller_ended",
		"payload": {"object": {
			"call_id": "end-1",
			"caller": {"phone_number": "+815054975111"},
			"callee": {"phone_number": "+819037241917"},
			"connected_start_time": "2026-05-09T19:22:45Z",
			"call_end_time":        "2026-05-09T19:22:50Z"
		}}
	}`
	req := httptest.NewRequest("POST", "/webhook/zoom", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	require.NotNil(t, ended)
	assert.Equal(t, "end-1", ended.CallID)
	assert.Equal(t, "outbound", ended.Direction)
	assert.Equal(t, 5, ended.DurationSec)
}

func TestWebhookHandler_MethodNotAllowed(t *testing.T) {
	handler := zoom.NewWebhookHandler("")
	req := httptest.NewRequest("GET", "/webhook/zoom", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestWebhookHandler_RecordingCompleted(t *testing.T) {
	handler := zoom.NewWebhookHandler("")
	var received zoom.RecordingCompletedEvent
	called := false
	handler.OnRecordingComplete(func(ev zoom.RecordingCompletedEvent) {
		received = ev
		called = true
	})

	// Zoom の実 payload は payload.object.recordings[] の配列形 (started は flat だが
	// completed は array、Zoom 側の inconsistency)。
	body := `{
		"event": "phone.recording_completed",
		"payload": {"object": {"recordings": [
			{"id": "rec-123", "call_id": "call-456", "download_url": "https://zoom.example/r/rec-123"}
		]}}
	}`
	req := httptest.NewRequest("POST", "/webhook/zoom", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.True(t, called, "OnRecordingComplete should fire")
	assert.Equal(t, "rec-123", received.RecordingID)
	assert.Equal(t, "call-456", received.CallID)
}
