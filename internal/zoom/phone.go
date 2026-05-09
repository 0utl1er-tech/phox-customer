package zoom

import (
	"encoding/json"
	"fmt"
	"net/url"
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
	ID            string `json:"id"`
	CallID        string `json:"call_id"`
	CallerName    string `json:"caller_name"`
	CallerNumber  string `json:"caller_number"`
	CalleeName    string `json:"callee_name"`
	CalleeNumber  string `json:"callee_number"`
	Direction     string `json:"direction"` // inbound / outbound
	Duration      int    `json:"duration"`  // seconds
	DownloadURL   string `json:"download_url"`
	DateTime      string `json:"date_time"`
	EndTime       string `json:"end_time"`
	RecordingType string `json:"recording_type"`
}

// ListAccountRecordings はアカウント全体の通話録音をページング取得する。
// from/to は YYYY-MM-DD 形式。Zoom 側の最大スパンは 1 ヶ月なので、長期間
// backfill する場合は呼び出し側で月単位に分割する。
//
// scope: phone:read:list_call_recordings:admin が必要。
func (c *Client) ListAccountRecordings(from, to string) ([]Recording, error) {
	const pageSize = 100
	var (
		out      []Recording
		nextPage string
	)
	for {
		path := fmt.Sprintf("/phone/recordings?from=%s&to=%s&page_size=%d", from, to, pageSize)
		if nextPage != "" {
			path += "&next_page_token=" + url.QueryEscape(nextPage)
		}
		body, status, err := c.doAPI("GET", path, nil)
		if err != nil {
			return nil, err
		}
		if status != 200 {
			return nil, fmt.Errorf("zoom: list account recordings %d: %s", status, string(body))
		}
		var resp struct {
			Recordings    []Recording `json:"recordings"`
			NextPageToken string      `json:"next_page_token"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("zoom: parse account recordings: %w", err)
		}
		out = append(out, resp.Recordings...)
		if resp.NextPageToken == "" {
			break
		}
		nextPage = resp.NextPageToken
	}
	return out, nil
}

// CallLog は phone.call_logs entry。recording_id がある場合のみ Recording API
// と紐づく。ListAccountRecordings で recording 経由で取れない場合 (録音失敗
// 等) の保険的な経路として使う。
type CallLog struct {
	ID              string `json:"id"`
	CallID          string `json:"call_id"`
	CallerName      string `json:"caller_name"`
	CallerNumber    string `json:"caller_number"`     // 内線番号 (例 "800")
	CallerDIDNumber string `json:"caller_did_number"` // E.164 番号 (例 "+815054975111")
	CalleeName      string `json:"callee_name"`
	CalleeNumber    string `json:"callee_number"`
	CalleeDIDNumber string `json:"callee_did_number"`
	Direction       string `json:"direction"`
	Duration        int    `json:"duration"`
	Result          string `json:"result"` // "Call connected" / "Call Cancel" / "Recorded" 等
	HasRecording    bool   `json:"has_recording"`
	HasVoicemail    bool   `json:"has_voicemail"`
	DateTime        string `json:"date_time"`
	CallEndTime     string `json:"call_end_time"`
	RecordingID     string `json:"recording_id"`
}

// ListAccountCallLogs はアカウント全体の通話ログをページング取得する。
// 録音されてない通話 (キャンセル含む) も全部返るので、Activity backfill
// のメインソースとして使う。
//
// scope: phone:read:list_call_logs:admin が必要。
func (c *Client) ListAccountCallLogs(from, to string) ([]CallLog, error) {
	const pageSize = 100
	var (
		out      []CallLog
		nextPage string
	)
	for {
		path := fmt.Sprintf("/phone/call_logs?from=%s&to=%s&page_size=%d", from, to, pageSize)
		if nextPage != "" {
			path += "&next_page_token=" + url.QueryEscape(nextPage)
		}
		body, status, err := c.doAPI("GET", path, nil)
		if err != nil {
			return nil, err
		}
		if status != 200 {
			return nil, fmt.Errorf("zoom: list account call_logs %d: %s", status, string(body))
		}
		var resp struct {
			CallLogs      []CallLog `json:"call_logs"`
			NextPageToken string    `json:"next_page_token"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("zoom: parse account call_logs: %w", err)
		}
		out = append(out, resp.CallLogs...)
		if resp.NextPageToken == "" {
			break
		}
		nextPage = resp.NextPageToken
	}
	return out, nil
}
