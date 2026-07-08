// Package mailu は mailu 管理 API (v1) の薄いクライアント。
// Phox がメールボックス (mailu アカウント) を自動プロビジョニングするために使う。
// 認証は Bearer トークン (MAILU_API_TOKEN)。
package mailu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrConflict は作成しようとしたアカウントが既に存在する場合 (HTTP 409)。
var ErrConflict = errors.New("mailu: account already exists")

// ErrNotFound は対象アカウントが存在しない場合 (HTTP 404)。
var ErrNotFound = errors.New("mailu: account not found")

// Client は mailu 管理 API クライアント。
type Client struct {
	base  string // 例: https://mail.0utl1er.tech/api/v1
	token string
	http  *http.Client
}

// NewClient は base か token が空なら nil を返す (プロビジョニング無効)。
func NewClient(base, token string) *Client {
	if base == "" || token == "" {
		return nil
	}
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Timeout: 20 * time.Second},
	}
}

// createUserBody は POST /user のリクエストボディ (使う項目のみ)。
type createUserBody struct {
	Email         string `json:"email"`
	RawPassword   string `json:"raw_password"`
	DisplayedName string `json:"displayed_name,omitempty"`
	Enabled       bool   `json:"enabled"`
	EnableIMAP    bool   `json:"enable_imap"`
	// なりすまし送信は Phox では使わない (各メールボックス本人として送る) ので
	// spoofing は許可しない。
	AllowSpoofing bool `json:"allow_spoofing"`
}

// CreateUser は mailu にメールボックスアカウントを作成する。
// 既存なら ErrConflict。
func (c *Client) CreateUser(ctx context.Context, email, rawPassword, displayName string) error {
	body := createUserBody{
		Email:         email,
		RawPassword:   rawPassword,
		DisplayedName: displayName,
		Enabled:       true,
		EnableIMAP:    true,
		AllowSpoofing: false,
	}
	return c.do(ctx, http.MethodPost, "/user", body)
}

// patchUserBody は PATCH /user/{email} — パスワード変更に使う。
type patchUserBody struct {
	RawPassword string `json:"raw_password,omitempty"`
}

// SetPassword は既存アカウントのパスワードを更新する。
func (c *Client) SetPassword(ctx context.Context, email, rawPassword string) error {
	return c.do(ctx, http.MethodPatch, "/user/"+url.PathEscape(email), patchUserBody{RawPassword: rawPassword})
}

// DeleteUser は mailu アカウントを削除する。既に無ければ ErrNotFound (無視可)。
func (c *Client) DeleteUser(ctx context.Context, email string) error {
	return c.do(ctx, http.MethodDelete, "/user/"+url.PathEscape(email), nil)
}

func (c *Client) do(ctx context.Context, method, path string, body any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("mailu: marshal: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return fmt.Errorf("mailu: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("mailu: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	switch resp.StatusCode {
	case http.StatusConflict:
		return ErrConflict
	case http.StatusNotFound:
		return ErrNotFound
	default:
		return fmt.Errorf("mailu: %s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
}
