package crypto_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/0utl1er-tech/phox-customer/internal/crypto"
)

func newCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	c, err := crypto.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	c := newCipher(t)
	plain := []byte("refresh_token_example_value")
	ct, err := c.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Equal(ct, plain) {
		t.Fatal("ciphertext equals plaintext")
	}
	pt, err := c.Decrypt(ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(pt, plain) {
		t.Fatalf("mismatch: got %q want %q", pt, plain)
	}
}

func TestEncryptIsNondeterministic(t *testing.T) {
	c := newCipher(t)
	ct1, _ := c.Encrypt([]byte("same input"))
	ct2, _ := c.Encrypt([]byte("same input"))
	if bytes.Equal(ct1, ct2) {
		t.Fatal("expected distinct ciphertexts due to random nonce")
	}
}

func TestDecryptShortCiphertextFails(t *testing.T) {
	c := newCipher(t)
	if _, err := c.Decrypt([]byte("tiny")); err == nil {
		t.Fatal("expected error for short ciphertext")
	}
}

func TestDecryptTamperFails(t *testing.T) {
	c := newCipher(t)
	ct, _ := c.Encrypt([]byte("tamper me"))
	ct[len(ct)-1] ^= 0xFF // flip last byte of tag
	if _, err := c.Decrypt(ct); err == nil {
		t.Fatal("expected auth tag failure")
	}
}

func TestNewCipherFromBase64(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	b64 := base64.StdEncoding.EncodeToString(key)
	c, err := crypto.NewCipherFromBase64(b64)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := c.Encrypt([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := c.Decrypt(ct)
	if err != nil {
		t.Fatal(err)
	}
	if string(pt) != "hello" {
		t.Fatalf("got %q", pt)
	}
}

func TestNewCipherRejectsWrongKeyLength(t *testing.T) {
	if _, err := crypto.NewCipher(make([]byte, 16)); err == nil {
		t.Fatal("expected error for 16-byte key")
	}
}
