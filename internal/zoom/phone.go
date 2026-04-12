package zoom

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PhoneUser は Zoom Phone ユーザー情報。
type PhoneUser struct {
	ID              string `json:"id"`
	Email           string `json:"email"`
	Name            string `json:"name"`
	ExtensionNumber string `json:"extension_number"`
	PhoneNumber     string `json:"phone_number"` // 正規化後の番号
}

// ListPhoneUsers は Zoom Phone ユーザー一覧を返す。
func (c *Client) ListPhoneUsers() ([]PhoneUser, error) {
	body, status, err := c.doAPI("GET", "/phone/users?page_size=100", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("zoom: list phone users %d: %s", status, string(body))
	}
	var resp struct {
		Users []struct {
			ID              string `json:"id"`
			Email           string `json:"email"`
			Name            string `json:"name"`
			ExtensionNumber string `json:"extension_number"`
			PhoneNumbers    []struct {
				Number string `json:"number"`
			} `json:"phone_numbers"`
		} `json:"users"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("zoom: parse phone users: %w", err)
	}

	out := make([]PhoneUser, 0, len(resp.Users))
	for _, u := range resp.Users {
		num := ""
		if len(u.PhoneNumbers) > 0 {
			num = u.PhoneNumbers[0].Number
		}
		out = append(out, PhoneUser{
			ID:              u.ID,
			Email:           u.Email,
			Name:            u.Name,
			ExtensionNumber: u.ExtensionNumber,
			PhoneNumber:     num,
		})
	}
	return out, nil
}

// FindPhoneUserByEmail は email で Zoom Phone ユーザーを探す。
// Keycloak email → Zoom email のマッピングに使う。
func (c *Client) FindPhoneUserByEmail(email string) (*PhoneUser, error) {
	users, err := c.ListPhoneUsers()
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(email)
	for _, u := range users {
		if strings.ToLower(u.Email) == lower {
			return &u, nil
		}
	}
	return nil, fmt.Errorf("zoom: phone user not found for email %q", email)
}

// MakeCall は Zoom Phone API で発信する。
// callerID は Zoom Phone ユーザーの ID (email ではなく Zoom user ID)。
// calleeNumber は E.164 形式の電話番号 (例: +81501234567)。
func (c *Client) MakeCall(callerUserID, calleeNumber string) (*CallInfo, error) {
	payload := fmt.Sprintf(`{"callee":"%s"}`, calleeNumber)
	body, status, err := c.doAPI("POST",
		fmt.Sprintf("/phone/users/%s/phone_calls", callerUserID),
		strings.NewReader(payload),
	)
	if err != nil {
		return nil, err
	}
	if status != 201 && status != 200 {
		return nil, fmt.Errorf("zoom: make call %d: %s", status, string(body))
	}

	var info CallInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("zoom: parse call response: %w", err)
	}
	return &info, nil
}

// CallInfo は Zoom Phone 発信 API のレスポンス。
type CallInfo struct {
	CallID string `json:"call_id"`
	Status string `json:"status"`
}

// GetCallRecordings は指定ユーザーの通話録音一覧を取得する。
func (c *Client) GetCallRecordings(userID string, from, to string) ([]Recording, error) {
	path := fmt.Sprintf("/phone/users/%s/recordings?from=%s&to=%s&page_size=50",
		userID, from, to)
	body, status, err := c.doAPI("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("zoom: get recordings %d: %s", status, string(body))
	}

	var resp struct {
		Recordings []Recording `json:"recordings"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("zoom: parse recordings: %w", err)
	}
	return resp.Recordings, nil
}

// Recording は Zoom Phone の通話録音情報。
type Recording struct {
	ID          string `json:"id"`
	CallID      string `json:"call_id"`
	CallerName  string `json:"caller_name"`
	CalleeName  string `json:"callee_name"`
	Direction   string `json:"direction"` // inbound / outbound
	Duration    int    `json:"duration"`   // seconds
	DownloadURL string `json:"download_url"`
	DateTime    string `json:"date_time"`
}
