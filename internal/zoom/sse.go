package zoom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// SSE 接続の keep-alive / 上限寿命。var にしているのはテストが短い値に
// 差し替えて heartbeat・deadline 経路を高速に検証できるようにするため。
var (
	sseHeartbeatInterval = 15 * time.Second
	sseMaxLifetime       = 30 * time.Minute
)

// CallNotification は phox-ui に SSE で push する着信通知。
type CallNotification struct {
	Type         string `json:"type"` // "ringing" | "answered" | "ended"
	CallID       string `json:"call_id"`
	CallerNumber string `json:"caller_number"`
	CallerName   string `json:"caller_name"`   // Zoom 側の名前
	CustomerID   string `json:"customer_id"`   // Phox で逆引きした Customer ID (未ヒットなら空)
	CustomerName string `json:"customer_name"` // Phox の Customer.name (未ヒットなら空)
	Direction    string `json:"direction"`     // "inbound" | "outbound"
	Timestamp    string `json:"timestamp"`
}

// SSEChannel は Broadcast を pod 跨ぎで配送するための Redis pub/sub channel 名。
// 単 pod 運用 (rdb == nil) では使われない。
const SSEChannel = "phox:sse:calls"

// sseEnvelope は Redis pub/sub に乗せる際の wrapper。userEmail と本体を JSON 化。
type sseEnvelope struct {
	UserEmail    string           `json:"user_email"`
	Notification CallNotification `json:"notification"`
}

// SSEHub は接続中のブラウザクライアントに SSE を配信するハブ。
// 各 CRM ユーザーが /sse/calls を購読し、着信通知をリアルタイムで受け取る。
//
// rdb が non-nil の場合は pod 跨ぎ配信モードで動作する:
//   - Broadcast() は Redis pub/sub channel "phox:sse:calls" に publish する
//   - Run(ctx) で起動した subscriber goroutine が channel を購読し、
//     受信した envelope を「自分の pod に居る」 SSE client に fan-out する
//
// rdb が nil の場合は in-memory のみ (単 pod 運用 or local dev) で動く。
type SSEHub struct {
	mu      sync.RWMutex
	clients map[string]map[chan CallNotification]bool // userEmail → set of channels
	rdb     *redis.Client                             // nil で in-memory モード
}

// NewSSEHub は in-memory hub を返す (単 pod / dev / test 用)。
func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients: make(map[string]map[chan CallNotification]bool),
	}
}

// NewRedisSSEHub は Redis pub/sub backed hub を返す。
// Run(ctx) を errgroup などに載せて subscriber goroutine を起動すること。
func NewRedisSSEHub(rdb *redis.Client) *SSEHub {
	return &SSEHub{
		clients: make(map[string]map[chan CallNotification]bool),
		rdb:     rdb,
	}
}

// Run は rdb 設定時のみ subscriber を回す long-running goroutine。
// rdb が nil の場合は何もせず即 nil 返却 (errgroup から呼んでも害なし)。
func (h *SSEHub) Run(ctx context.Context) error {
	if h.rdb == nil {
		return nil
	}
	sub := h.rdb.Subscribe(ctx, SSEChannel)
	defer sub.Close()

	// 接続確認 (REDIS_ADDR が間違ってる場合に起動 fail させる)。
	if _, err := sub.Receive(ctx); err != nil {
		return fmt.Errorf("sse: redis subscribe %s: %w", SSEChannel, err)
	}
	log.Info().Str("channel", SSEChannel).Msg("SSE hub: redis subscriber started")
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case m, ok := <-ch:
			if !ok {
				return nil
			}
			var env sseEnvelope
			if err := json.Unmarshal([]byte(m.Payload), &env); err != nil {
				log.Warn().Err(err).Msg("SSE hub: bad redis payload, dropping")
				continue
			}
			h.localBroadcast(env.UserEmail, env.Notification)
		}
	}
}

// Subscribe は userEmail の SSE クライアントを登録し、通知チャネルを返す。
// 切断時は Unsubscribe を呼ぶこと。
func (h *SSEHub) Subscribe(userEmail string) chan CallNotification {
	ch := make(chan CallNotification, 16)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[userEmail] == nil {
		h.clients[userEmail] = make(map[chan CallNotification]bool)
	}
	h.clients[userEmail][ch] = true
	log.Debug().Str("user", userEmail).Msg("sse: client subscribed")
	return ch
}

// Unsubscribe は SSE クライアントを解除する。
func (h *SSEHub) Unsubscribe(userEmail string, ch chan CallNotification) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.clients[userEmail]; ok {
		delete(set, ch)
		if len(set) == 0 {
			delete(h.clients, userEmail)
		}
	}
	close(ch)
}

// Broadcast は指定ユーザーの全 SSE クライアントに通知を送る。
// userEmail が空なら全ユーザーに送信 (global broadcast)。
//
// rdb 設定時は redis pub/sub に publish するだけ。各 pod の subscriber が
// 受け取って localBroadcast でローカル client に fan-out する。
// rdb 未設定時は直接 localBroadcast を呼ぶ (= 単 pod 運用)。
func (h *SSEHub) Broadcast(userEmail string, n CallNotification) {
	if h.rdb != nil {
		payload, err := json.Marshal(sseEnvelope{UserEmail: userEmail, Notification: n})
		if err != nil {
			log.Warn().Err(err).Msg("SSE hub: marshal envelope")
			return
		}
		// Publish は失敗しても呼び出し元 (webhook handler) を落とさない。
		// SSE は best-effort 配信。
		if err := h.rdb.Publish(context.Background(), SSEChannel, payload).Err(); err != nil {
			log.Warn().Err(err).Msg("SSE hub: redis publish failed")
		}
		return
	}
	h.localBroadcast(userEmail, n)
}

// localBroadcast は redis を経由せず自分の pod 内 client にだけ届ける。
// 直接呼ばれるのは:
//   - rdb 未設定時の Broadcast() から
//   - rdb 設定時の Run() subscriber goroutine から
func (h *SSEHub) localBroadcast(userEmail string, n CallNotification) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	send := func(set map[chan CallNotification]bool) {
		for ch := range set {
			select {
			case ch <- n:
			default:
				// チャネルが詰まってたら drop (slow client)
			}
		}
	}

	if userEmail != "" {
		if set, ok := h.clients[userEmail]; ok {
			send(set)
		}
	} else {
		for _, set := range h.clients {
			send(set)
		}
	}
}

// ServeHTTP は `GET /sse/calls` の SSE endpoint。
// クエリパラメータ `email` で購読対象ユーザーを指定する。
// 認証は Keycloak JWT を Authorization ヘッダから検証する (呼び出し側で middleware 適用)。
func (h *SSEHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	userEmail := r.URL.Query().Get("email")
	if userEmail == "" {
		http.Error(w, "email query param required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := h.Subscribe(userEmail)
	defer h.Unsubscribe(userEmail, ch)

	// 接続直後に ping を送って SSE 接続を確立
	if _, err := fmt.Fprintf(w, "event: ping\ndata: {\"time\":\"%s\"}\n\n", time.Now().Format(time.RFC3339)); err != nil {
		return
	}
	flusher.Flush()

	// ── なぜ heartbeat が要るか ─────────────────────────────────────
	// このハンドラは r.Context().Done() だけでクライアント切断を検知して
	// いたが、Cilium Gateway (Envoy) → backend が h2c で多重化される経路
	// では、ブラウザが去っても Envoy が upstream ストリームを即クローズ
	// しないことがあり、Done() が発火せず goroutine と HTTP/2 ストリームが
	// リークする。ストリームが Go http2 の MaxConcurrentStreams (既定 250)
	// に達すると、その接続上の新規 RPC が全てブロックする (実証 2026-07-02,
	// phox-e2e フルスイート後半が軒並みタイムアウト; 280 SSE で backend RSS
	// が増えたまま戻らないことを確認)。
	//
	// 対策: 定期的に comment ping を書き、書き込みエラー (= 相手がいない)
	// で確実に抜ける。さらに絶対上限寿命を設けて「永遠に生きる接続」を無く
	// す (UI 側の EventSource が自動再接続するので UX 影響なし)。
	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()
	deadline := time.NewTimer(sseMaxLifetime)
	defer deadline.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			// 上限到達。クライアントは EventSource の自動再接続で張り直す。
			return
		case <-heartbeat.C:
			// SSE コメント行 (":") は仕様上クライアントに無視される keep-alive。
			// 書き込みが失敗したら相手はもういない → 抜けて Unsubscribe。
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case n, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(n)
			if _, err := fmt.Fprintf(w, "event: call\ndata: %s\n\n", string(data)); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// SetSSEHeartbeatIntervalForTest は heartbeat 間隔を一時的に差し替え、元に
// 戻す restore 関数を返す (外部テストパッケージ用)。
func SetSSEHeartbeatIntervalForTest(d time.Duration) func() {
	old := sseHeartbeatInterval
	sseHeartbeatInterval = d
	return func() { sseHeartbeatInterval = old }
}

// SetSSEMaxLifetimeForTest は上限寿命を一時的に差し替える (外部テスト用)。
func SetSSEMaxLifetimeForTest(d time.Duration) func() {
	old := sseMaxLifetime
	sseMaxLifetime = d
	return func() { sseMaxLifetime = old }
}

// ClientCount は現在購読中の SSE クライアント総数を返す (テスト・観測用)。
func (h *SSEHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	total := 0
	for _, set := range h.clients {
		total += len(set)
	}
	return total
}
