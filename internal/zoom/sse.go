package zoom

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// CallNotification は phox-ui に SSE で push する着信通知。
type CallNotification struct {
	Type         string `json:"type"`          // "ringing" | "answered" | "ended"
	CallID       string `json:"call_id"`
	CallerNumber string `json:"caller_number"`
	CallerName   string `json:"caller_name"`   // Zoom 側の名前
	CustomerID   string `json:"customer_id"`   // Phox で逆引きした Customer ID (未ヒットなら空)
	CustomerName string `json:"customer_name"` // Phox の Customer.name (未ヒットなら空)
	Direction    string `json:"direction"`     // "inbound" | "outbound"
	Timestamp    string `json:"timestamp"`
}

// SSEHub は接続中のブラウザクライアントに SSE を配信するハブ。
// 各 CRM ユーザーが /sse/calls を購読し、着信通知をリアルタイムで受け取る。
type SSEHub struct {
	mu      sync.RWMutex
	clients map[string]map[chan CallNotification]bool // userEmail → set of channels
}

func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients: make(map[string]map[chan CallNotification]bool),
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
func (h *SSEHub) Broadcast(userEmail string, n CallNotification) {
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
	fmt.Fprintf(w, "event: ping\ndata: {\"time\":\"%s\"}\n\n", time.Now().Format(time.RFC3339))
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case n, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(n)
			fmt.Fprintf(w, "event: call\ndata: %s\n\n", string(data))
			flusher.Flush()
		}
	}
}
