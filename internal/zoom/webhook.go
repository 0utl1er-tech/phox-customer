package zoom

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"
)

// WebhookEvent は Zoom Webhook の共通エンベロープ。
type WebhookEvent struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
	// Zoom URL validation 用
	PlainToken string `json:"plainToken,omitempty"`
}

// PhoneCallEvent は phone.callee_ringing / phone.callee_answered / phone.callee_ended の payload。
//
// `phone.callee_ended` には DurationSec (= 通話秒数) も含まれる。
// `start_time` / `answer_time` / `end_time` は ISO8601 で、Activity.occurred_at の
// ベースとして使う (= end_time、無ければ answer_time、無ければ DateTime)。
type PhoneCallEvent struct {
	CallID       string `json:"call_id"`
	CallerNumber string `json:"caller_phone_number"`
	CallerName   string `json:"caller_name"`
	CalleeNumber string `json:"callee_phone_number"`
	CalleeName   string `json:"callee_name"`
	Direction    string `json:"direction"` // inbound / outbound
	DateTime     string `json:"date_time"`
	StartTime    string `json:"start_time"`
	AnswerTime   string `json:"answer_time"`
	EndTime      string `json:"end_time"`
	DurationSec  int    `json:"duration"` // seconds, callee_ended only
	UserID       string `json:"user_id"`
	UserEmail    string `json:"user_email"`
}

// RecordingCompletedEvent は phone.recording_completed の payload (object 内)。
// download_url は Zoom OAuth Bearer 必須、有効期限 ~ 15 分。
type RecordingCompletedEvent struct {
	CallID       string `json:"call_id"`
	RecordingID  string `json:"recording_id"`
	DownloadURL  string `json:"download_url"`
	Duration     int    `json:"duration"` // seconds
	DateTime     string `json:"date_time"`
	CallerNumber string `json:"caller_phone_number"`
	CalleeNumber string `json:"callee_phone_number"`
}

// IncomingCallHandler は着信通知を受け取るコールバック型。
// phox-customer 側で Customer 逆引き + UI Push に使う。
type IncomingCallHandler func(event PhoneCallEvent)

// RecordingCompletedHandler は録音完了 (download_url 利用可) を受け取る。
type RecordingCompletedHandler func(event RecordingCompletedEvent)

// WebhookHandler は Zoom Webhook HTTP handler。
type WebhookHandler struct {
	secretToken         string // Zoom App の Secret Token (webhook signature 検証)
	onIncomingRinging   IncomingCallHandler
	onCallAnswered      IncomingCallHandler
	onCallEnded         IncomingCallHandler
	onRecordingComplete RecordingCompletedHandler
}

// NewWebhookHandler は webhook handler を作成する。
// secretToken が空なら署名検証をスキップする (dev 用)。
func NewWebhookHandler(secretToken string) *WebhookHandler {
	return &WebhookHandler{
		secretToken: secretToken,
	}
}

func (h *WebhookHandler) OnIncomingRinging(fn IncomingCallHandler) {
	h.onIncomingRinging = fn
}

func (h *WebhookHandler) OnCallAnswered(fn IncomingCallHandler) {
	h.onCallAnswered = fn
}

func (h *WebhookHandler) OnCallEnded(fn IncomingCallHandler) {
	h.onCallEnded = fn
}

// OnRecordingComplete は phone.recording_completed の構造化ペイロードを
// 受け取る handler を登録する。Phase 22 から JSON 直渡しではなく typed。
func (h *WebhookHandler) OnRecordingComplete(fn RecordingCompletedHandler) {
	h.onRecordingComplete = fn
}

// ServeHTTP は Zoom からの Webhook POST を処理する。
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	// Zoom URL Validation challenge (App 作成時の endpoint 検証)
	var env WebhookEvent
	if err := json.Unmarshal(body, &env); err != nil {
		http.Error(w, "parse json", http.StatusBadRequest)
		return
	}

	// URL validation: Zoom が plainToken を送ってきたら、HMAC-SHA256 で応答する
	if env.Event == "endpoint.url_validation" && env.PlainToken != "" {
		hash := hmacSHA256(h.secretToken, env.PlainToken)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"plainToken":     env.PlainToken,
			"encryptedToken": hash,
		})
		log.Info().Msg("zoom webhook: URL validation responded")
		return
	}

	// Signature 検証 (secretToken が設定されている場合)
	if h.secretToken != "" {
		ts := r.Header.Get("x-zm-request-timestamp")
		sig := r.Header.Get("x-zm-signature")
		if ts != "" && sig != "" {
			msg := fmt.Sprintf("v0:%s:%s", ts, string(body))
			expected := "v0=" + hmacSHA256(h.secretToken, msg)
			if sig != expected {
				log.Warn().Str("event", env.Event).Msg("zoom webhook: signature mismatch")
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
		}
	}

	// イベント分岐
	switch env.Event {
	case "phone.callee_ringing":
		h.handlePhoneEvent(env.Payload, h.onIncomingRinging, "ringing")
	case "phone.callee_answered":
		h.handlePhoneEvent(env.Payload, h.onCallAnswered, "answered")
	case "phone.callee_ended", "phone.caller_ended":
		h.handlePhoneEvent(env.Payload, h.onCallEnded, "ended")
	case "phone.recording_completed":
		h.handleRecordingCompleted(env.Payload)
	default:
		log.Debug().Str("event", env.Event).Msg("zoom webhook: unhandled event")
	}

	w.WriteHeader(http.StatusOK)
}

func (h *WebhookHandler) handlePhoneEvent(payload json.RawMessage, handler IncomingCallHandler, label string) {
	if handler == nil {
		return
	}
	var obj struct {
		Object PhoneCallEvent `json:"object"`
	}
	if err := json.Unmarshal(payload, &obj); err != nil {
		log.Warn().Err(err).Str("label", label).Msg("zoom webhook: parse phone event")
		return
	}
	log.Info().
		Str("event", label).
		Str("call_id", obj.Object.CallID).
		Str("caller", obj.Object.CallerNumber).
		Str("callee", obj.Object.CalleeNumber).
		Str("direction", obj.Object.Direction).
		Int("duration", obj.Object.DurationSec).
		Msg("zoom webhook: phone event")
	handler(obj.Object)
}

func (h *WebhookHandler) handleRecordingCompleted(payload json.RawMessage) {
	if h.onRecordingComplete == nil {
		log.Debug().Msg("zoom webhook: recording_completed (no handler registered)")
		return
	}
	var obj struct {
		Object RecordingCompletedEvent `json:"object"`
	}
	if err := json.Unmarshal(payload, &obj); err != nil {
		log.Warn().Err(err).Msg("zoom webhook: parse recording_completed")
		return
	}
	log.Info().
		Str("call_id", obj.Object.CallID).
		Str("recording_id", obj.Object.RecordingID).
		Int("duration", obj.Object.Duration).
		Msg("zoom webhook: recording completed")
	h.onRecordingComplete(obj.Object)
}

func hmacSHA256(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// NormalizeJapanesePhone は日本の電話番号を E.164 っぽい形式に正規化する。
// 例: "03-1234-5678" → "+81312345678", "090-1234-5678" → "+819012345678"
//
// 注: Customer/Contact マッチングには PhoneToDigits (末尾 10 桁) を使うのが
// 推奨。NormalizeJapanesePhone は表示用 / 既存ロジック互換用。
func NormalizeJapanesePhone(phone string) string {
	// ハイフン・スペース・括弧を除去
	clean := ""
	for _, r := range phone {
		if r >= '0' && r <= '9' || r == '+' {
			clean += string(r)
		}
	}
	if clean == "" {
		return phone
	}
	// 既に +81 で始まっていればそのまま
	if len(clean) > 3 && clean[:3] == "+81" {
		return clean
	}
	// 0 で始まっていれば +81 に置換
	if clean[0] == '0' {
		return "+81" + clean[1:]
	}
	return clean
}
