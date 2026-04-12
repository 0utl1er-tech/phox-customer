// Package zoom は Zoom Phone API との通信を担う。
// Server-to-Server OAuth (account_credentials grant) でトークンを取得し、
// Phone API を叩く。トークンは 1 時間有効なのでキャッシュして再利用する。
package zoom

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Config は Zoom Server-to-Server OAuth の認証情報。
type Config struct {
	AccountID    string
	ClientID     string
	ClientSecret string
}

// Enabled は credentials が設定されているか。
func (c Config) Enabled() bool {
	return c.AccountID != "" && c.ClientID != "" && c.ClientSecret != ""
}

// Client は Zoom API クライアント。トークンを自動キャッシュ + refresh する。
type Client struct {
	cfg Config

	mu          sync.Mutex
	accessToken string
	expiry      time.Time
}

// NewClient は Zoom API Client を返す。cfg.Enabled() == false なら nil を返す。
func NewClient(cfg Config) *Client {
	if !cfg.Enabled() {
		return nil
	}
	return &Client{cfg: cfg}
}

const tokenURL = "https://zoom.us/oauth/token"
const apiBase = "https://api.zoom.us/v2"

// token はキャッシュ済みトークンを返す。有効期限が近ければ refresh する。
func (c *Client) token() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 有効期限の 60 秒前にはリフレッシュ (安全マージン)
	if c.accessToken != "" && time.Now().Before(c.expiry.Add(-60*time.Second)) {
		return c.accessToken, nil
	}

	data := url.Values{}
	data.Set("grant_type", "account_credentials")
	data.Set("account_id", c.cfg.AccountID)

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("zoom: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.cfg.ClientID, c.cfg.ClientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("zoom: token request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("zoom: token %d: %s", resp.StatusCode, string(body))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("zoom: parse token: %w", err)
	}
	if tok.AccessToken == "" {
		return "", errors.New("zoom: empty access_token in response")
	}

	c.accessToken = tok.AccessToken
	c.expiry = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	log.Debug().Int("expires_in", tok.ExpiresIn).Msg("zoom: token refreshed")
	return c.accessToken, nil
}

// doAPI は認証済み Zoom API リクエストを実行する。
func (c *Client) doAPI(method, path string, reqBody io.Reader) ([]byte, int, error) {
	tok, err := c.token()
	if err != nil {
		return nil, 0, err
	}

	u := apiBase + path
	req, err := http.NewRequest(method, u, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("zoom: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("zoom: request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}
