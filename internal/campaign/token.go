// Package campaign implements the cold-email campaign engine (Phase 27):
// the paced send worker, HMAC tracking tokens, template rendering with the
// 特定電子メール法 footer, and the public unsubscribe/tracking HTTP handlers.
package campaign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"

	"github.com/google/uuid"
)

// トークン種別 (1 byte)。
const (
	KindOpen        byte = 'o'
	KindClick       byte = 'c'
	KindUnsubscribe byte = 'u'
)

const sigLen = 16 // HMAC-SHA256 の先頭 16 byte (128 bit) で十分

// Tokenizer は非認証トラッキングエンドポイント用の推測不能トークンを
// 発行/検証する。レイアウトは
//
//	base64url( recipient_uuid[16] || kind[1] || link_idx[2 BE] || HMAC-SHA256(key, 前 19 byte)[:16] )
//
// URL は DB (CampaignLink) 持ちでトークンには乗せない — open redirect を
// 作らないため。鍵は CAMPAIGN_TRACKING_KEY (base64 32 byte)。
// recording-signing (internal/recording) と同じく hmac.Equal で定数時間比較。
type Tokenizer struct {
	key []byte
}

// NewTokenizer returns nil when key is empty (tracking disabled).
func NewTokenizer(key []byte) *Tokenizer {
	if len(key) == 0 {
		return nil
	}
	return &Tokenizer{key: key}
}

// Token は署名付きトークンを発行する。idx はクリックトラッキングの
// CampaignLink.idx (open/unsubscribe では 0)。
func (t *Tokenizer) Token(kind byte, recipientID uuid.UUID, idx uint16) string {
	payload := make([]byte, 0, 19+sigLen)
	payload = append(payload, recipientID[:]...)
	payload = append(payload, kind)
	payload = binary.BigEndian.AppendUint16(payload, idx)
	mac := hmac.New(sha256.New, t.key)
	mac.Write(payload)
	payload = append(payload, mac.Sum(nil)[:sigLen]...)
	return base64.RawURLEncoding.EncodeToString(payload)
}

var ErrInvalidToken = errors.New("campaign: invalid tracking token")

// Parse はトークンを検証して分解する。署名不一致・長さ不正は ErrInvalidToken。
func (t *Tokenizer) Parse(token string) (kind byte, recipientID uuid.UUID, idx uint16, err error) {
	raw, derr := base64.RawURLEncoding.DecodeString(token)
	if derr != nil || len(raw) != 19+sigLen {
		return 0, uuid.Nil, 0, ErrInvalidToken
	}
	payload, sig := raw[:19], raw[19:]
	mac := hmac.New(sha256.New, t.key)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)[:sigLen]) {
		return 0, uuid.Nil, 0, ErrInvalidToken
	}
	copy(recipientID[:], payload[:16])
	return payload[16], recipientID, binary.BigEndian.Uint16(payload[17:19]), nil
}
