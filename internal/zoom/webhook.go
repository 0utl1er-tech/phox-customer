package zoom

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// WebhookEvent は Zoom Webhook の共通エンベロープ。
type WebhookEvent struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
}

// urlValidationPayload は endpoint.url_validation event の payload。
// Zoom は `{"event":"...","payload":{"plainToken":"..."}}` の形で送ってくる
// ので、top-level ではなく payload 内から取り出す必要がある。
// (top-level に PlainToken を置く実装ミスのせいで URL 検証応答が空 body の
// 200 で返り、Zoom Marketplace 側の event subscription が登録不能になっていた)
type urlValidationPayload struct {
	PlainToken string `json:"plainToken"`
}

// CallParty は phone.{caller,callee}_* event の payload.object.{caller,callee}
// 共通形。Zoom の癖メモ:
//   - extension_number は数値 (extension_type=user の社内線でも、pstn の外部番号でも int)
//   - extension_type で user / pstn / common_area / call_queue 等が区別される
//   - phone_number は E.164 (+81…) で来る
type CallParty struct {
	Name            string `json:"name"`
	UserID          string `json:"user_id"`
	UserEmail       string `json:"user_email"`
	ExtensionID     string `json:"extension_id"`
	ExtensionType   string `json:"extension_type"`
	ExtensionNumber int64  `json:"extension_number"`
	PhoneNumber     string `json:"phone_number"`
	ConnectionType  string `json:"connection_type"`
	DeviceType      string `json:"device_type"`
	DeviceName      string `json:"device_name"`
	Timezone        string `json:"timezone"`
}

// PhoneCallEvent は phone.callee_* / phone.caller_* event の payload.object。
//
// 実際の Zoom payload で確認した shape:
//   - caller / callee は **ネストされた CallParty オブジェクト** (旧版の flat
//     `caller_phone_number` などは存在しない)
//   - direction フィールド自体は無く、event 名 `phone.caller_*` (発信側 = staff
//     が caller) か `phone.callee_*` (受信側 = staff が callee) で判別する
//   - duration は無く、ringing/connected/end の時刻差から計算する
//   - `handup_result` は Zoom 側の typo (hangup ではない)。"Call connected" 等
//
// Direction / DurationSec は JSON ではなく、handler が event 名 と時刻から
// 後付けで埋める (consumer 側のロジックを単純にするため)。
type PhoneCallEvent struct {
	CallID             string    `json:"call_id"`
	Caller             CallParty `json:"caller"`
	Callee             CallParty `json:"callee"`
	RingingStartTime   string    `json:"ringing_start_time"`
	ConnectedStartTime string    `json:"connected_start_time"`
	CallEndTime        string    `json:"call_end_time"`
	HangupResult       string    `json:"handup_result"`

	// 後付けフィールド (JSON tag 無し):
	Direction   string `json:"-"` // "inbound" / "outbound" — event 名から導出
	DurationSec int    `json:"-"` // CallEndTime - ConnectedStartTime
}

// RecordingCompletedEvent は phone.recording_completed / phone.recording_started
// の payload.object。phone.callee_*/caller_* と違って caller/callee が nest
// していない (flat な caller_number / callee_number を直接持つ)。
type RecordingCompletedEvent struct {
	// id は recording 自体の ID。call_id とは別物。
	RecordingID  string `json:"id"`
	CallID       string `json:"call_id"`
	UserID       string `json:"user_id"`
	CallerNumber string `json:"caller_number"`
	CalleeNumber string `json:"callee_number"`
	Direction    string `json:"direction"`
	DateTime     string `json:"date_time"`
	// Recording type: "Automatic" / "OnDemand" / "Adhoc" 等。
	RecordingType string `json:"recording_type"`
	// download_url は recording_completed のみ含まれる短期 URL (15 分有効、
	// Zoom OAuth Bearer 必須)。recording_started には含まれない。
	DownloadURL string `json:"download_url"`
	Duration    int    `json:"duration"` // seconds
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

	// URL validation: Zoom が plainToken を送ってきたら、HMAC-SHA256 で応答する。
	// plainToken は payload 内に入ってるので env.Payload から再パースする。
	if env.Event == "endpoint.url_validation" {
		var v urlValidationPayload
		if err := json.Unmarshal(env.Payload, &v); err != nil || v.PlainToken == "" {
			http.Error(w, "invalid url_validation payload", http.StatusBadRequest)
			return
		}
		hash := hmacSHA256(h.secretToken, v.PlainToken)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"plainToken":     v.PlainToken,
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

	// 受信した event の生 payload を debug log に残す (将来 Zoom が schema を
	// 拡張した時の調査用)。debug level なので production では普段見えない。
	log.Debug().Str("event", env.Event).RawJSON("payload", env.Payload).Msg("zoom webhook: raw payload")

	// イベント分岐。caller_* と callee_* で direction が反対 (caller = phox 側
	// が発信、callee = phox 側が受信) なので両方を載せて handler に渡す。
	switch env.Event {
	case "phone.callee_ringing":
		h.handlePhoneEvent(env.Payload, h.onIncomingRinging, "ringing", "inbound")
	case "phone.caller_ringing":
		h.handlePhoneEvent(env.Payload, h.onIncomingRinging, "ringing", "outbound")
	case "phone.callee_answered":
		h.handlePhoneEvent(env.Payload, h.onCallAnswered, "answered", "inbound")
	case "phone.caller_connected":
		// Zoom は caller 側を caller_connected と呼ぶ (callee_answered と対)
		h.handlePhoneEvent(env.Payload, h.onCallAnswered, "answered", "outbound")
	case "phone.callee_ended":
		h.handlePhoneEvent(env.Payload, h.onCallEnded, "ended", "inbound")
	case "phone.caller_ended":
		h.handlePhoneEvent(env.Payload, h.onCallEnded, "ended", "outbound")
	case "phone.recording_completed":
		h.handleRecordingCompleted(env.Payload)
	default:
		log.Debug().Str("event", env.Event).Msg("zoom webhook: unhandled event")
	}

	w.WriteHeader(http.StatusOK)
}

func (h *WebhookHandler) handlePhoneEvent(
	payload json.RawMessage,
	handler IncomingCallHandler,
	label, direction string,
) {
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
	// Direction は event 名 (caller_* / callee_*) から導出して付与。
	obj.Object.Direction = direction
	// DurationSec は connected_start_time → call_end_time の差。両方無いと 0。
	obj.Object.DurationSec = computeDuration(obj.Object.ConnectedStartTime, obj.Object.CallEndTime)

	log.Info().
		Str("event", label).
		Str("direction", direction).
		Str("call_id", obj.Object.CallID).
		Str("caller", obj.Object.Caller.PhoneNumber).
		Str("callee", obj.Object.Callee.PhoneNumber).
		Int("duration", obj.Object.DurationSec).
		Msg("zoom webhook: phone event")
	handler(obj.Object)
}

// computeDuration は ISO8601 二点間の秒数。parse 失敗 / 順序逆転 / 片方空 で 0。
func computeDuration(startISO, endISO string) int {
	if startISO == "" || endISO == "" {
		return 0
	}
	start, err1 := time.Parse(time.RFC3339, startISO)
	end, err2 := time.Parse(time.RFC3339, endISO)
	if err1 != nil || err2 != nil {
		return 0
	}
	d := int(end.Sub(start).Seconds())
	if d < 0 {
		return 0
	}
	return d
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
