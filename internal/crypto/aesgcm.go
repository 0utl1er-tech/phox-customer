// Package crypto は AES-GCM による対称暗号化ユーティリティを提供する。
// 用途は UserGoogleToken の refresh_token を DB 保存前に暗号化すること。
//
// 鍵 (32 byte) は env `GCAL_TOKEN_KEY` から base64 で読み込まれる。
// nonce は暗号化ごとにランダム生成し、ciphertext の先頭に埋め込む:
//
//	output = nonce (12 byte) || ciphertext || tag
//
// Decrypt は先頭 12 byte を nonce として切り出す。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Cipher wraps a pre-computed AES-GCM AEAD.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher creates a Cipher from a 32-byte AES-256 key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: cipher.NewGCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// NewCipherFromBase64 decodes a base64-encoded 32-byte key and returns a Cipher.
// Used to wire env var `GCAL_TOKEN_KEY`.
func NewCipherFromBase64(b64 string) (*Cipher, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		// 一部 env 保管では url-safe / no-pad が使われる可能性 — fallback
		raw2, err2 := base64.RawStdEncoding.DecodeString(b64)
		if err2 != nil {
			return nil, fmt.Errorf("crypto: base64 decode key: %w", err)
		}
		raw = raw2
	}
	return NewCipher(raw)
}

// Encrypt returns nonce||ciphertext||tag for the given plaintext.
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("crypto: nil cipher")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: read nonce: %w", err)
	}
	// Seal appends ciphertext to nonce (prealloc) so the result is nonce||ct||tag.
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt undoes Encrypt. Returns error if the ciphertext is too short or
// the authentication tag fails.
func (c *Cipher) Decrypt(ct []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("crypto: nil cipher")
	}
	ns := c.aead.NonceSize()
	if len(ct) < ns {
		return nil, errors.New("crypto: ciphertext too short")
	}
	nonce := ct[:ns]
	body := ct[ns:]
	pt, err := c.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: aead.Open: %w", err)
	}
	return pt, nil
}

// EncryptString は string → encrypted bytes のショートカット。
func (c *Cipher) EncryptString(s string) ([]byte, error) {
	return c.Encrypt([]byte(s))
}

// DecryptString は encrypted bytes → string のショートカット。
func (c *Cipher) DecryptString(ct []byte) (string, error) {
	pt, err := c.Decrypt(ct)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
