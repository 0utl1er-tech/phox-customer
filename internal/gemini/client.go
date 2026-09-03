// Package gemini は Google Generative Language API (Gemini) を net/http で
// 直接叩く小さなクライアント。Phase 27j の CSV 取り込みデータ補完に使う。
// SDK 依存は増やさない方針 (structured output の JSON schema を投げるだけなので
// 素の HTTP で十分)。
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	defaultAPIBase = "https://generativelanguage.googleapis.com/v1beta"
	// DefaultModel は GEMINI_MODEL 未設定時に使う安価なモデル。
	DefaultModel = "gemini-2.0-flash"

	// batchSize は 1 回の generateContent に載せる CSV 行数。
	// 大きくしすぎると出力 JSON が崩れやすくなるので控えめにする。
	batchSize = 20

	// maxCellLen は Gemini に渡す前に各セルを切り詰める長さ (コスト対策)。
	maxCellLen = 300
)

// CanonicalFields は補完対象の canonical フィールド名 (Customer の import 列)。
var CanonicalFields = []string{"phone", "category", "name", "corporation", "address", "memo", "mail"}

// Client は Gemini API クライアント。apiKey が無ければ NewClient は nil を返し、
// 呼び出し側は Enabled() (nil-safe) で機能を無効化する。
type Client struct {
	apiKey  string
	model   string
	apiBase string // テスト時に httptest サーバへ差し替え可能
	http    *http.Client
}

// Option は NewClient の任意設定。
type Option func(*Client)

// WithAPIBase は API ベース URL を差し替える (主にテスト用)。
func WithAPIBase(base string) Option {
	return func(c *Client) { c.apiBase = base }
}

// NewClient は Gemini クライアントを返す。apiKey が空なら nil を返す。
func NewClient(apiKey, model string, opts ...Option) *Client {
	if apiKey == "" {
		return nil
	}
	if model == "" {
		model = DefaultModel
	}
	c := &Client{
		apiKey:  apiKey,
		model:   model,
		apiBase: defaultAPIBase,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Enabled は API キーが設定されているか (nil レシーバ safe)。
func (c *Client) Enabled() bool { return c != nil && c.apiKey != "" }

// Model は使用するモデル名を返す (nil レシーバ safe)。
func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

// RowResult は 1 行分の正規化結果。Fields のキーは CanonicalFields。
// 根拠がなく補完できないフィールドは含まれない (または空文字)。
type RowResult struct {
	Index      int               // 入力 rows における 0-based 添字
	Fields     map[string]string // canonical field → 正規化済み値
	Confidence float64           // 0.0-1.0
}

// NormalizeRows は CSV のヘッダ + 生の行データを Gemini で正規化する。
// 内部で batchSize 行ずつ直列に分割して呼び出す (レート・出力崩れ対策)。
// 一部バッチの失敗は warning ログの上スキップし、成功分だけ返す。
// 全バッチが失敗した場合のみ error を返す。
func (c *Client) NormalizeRows(ctx context.Context, headers []string, rows [][]string, targetFields []string) ([]RowResult, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("gemini: client not configured")
	}
	if len(targetFields) == 0 {
		targetFields = CanonicalFields
	}

	var out []RowResult
	var lastErr error
	failed := 0
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		results, err := c.normalizeBatch(ctx, headers, rows[start:end], targetFields, start)
		if err != nil {
			// context キャンセルは即座に打ち切る
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			log.Warn().Err(err).Int("batch_start", start).Msg("gemini: batch normalize failed, skipping")
			lastErr = err
			failed++
			continue
		}
		out = append(out, results...)
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	if failed > 0 {
		log.Warn().Int("failed_batches", failed).Int("ok_rows", len(out)).Msg("gemini: some batches failed")
	}
	return out, nil
}

// ---- Gemini generateContent request/response 型 (必要最小限) ----

type genRequest struct {
	Contents         []genContent `json:"contents"`
	GenerationConfig genGenConfig `json:"generationConfig"`
	SafetySettings   []genSafety  `json:"safetySettings,omitempty"`
	SystemInstr      *genContent  `json:"systemInstruction,omitempty"`
}

type genContent struct {
	Role  string    `json:"role,omitempty"`
	Parts []genPart `json:"parts"`
}

type genPart struct {
	Text string `json:"text"`
}

type genGenConfig struct {
	Temperature      float64         `json:"temperature"`
	ResponseMimeType string          `json:"responseMimeType"`
	ResponseSchema   json.RawMessage `json:"responseSchema,omitempty"`
	MaxOutputTokens  int             `json:"maxOutputTokens,omitempty"`
}

type genSafety struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type genResponse struct {
	Candidates []struct {
		Content struct {
			Parts []genPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// rowOut は structured output で受け取る 1 行分の JSON。
type rowOut struct {
	I           int     `json:"i"`
	Phone       string  `json:"phone"`
	Category    string  `json:"category"`
	Name        string  `json:"name"`
	Corporation string  `json:"corporation"`
	Address     string  `json:"address"`
	Memo        string  `json:"memo"`
	Mail        string  `json:"mail"`
	Confidence  float64 `json:"confidence"`
}

// responseSchema は Gemini structured output の JSON schema。
// (Gemini の型名は大文字: ARRAY/OBJECT/STRING/INTEGER/NUMBER)
const responseSchema = `{
  "type": "ARRAY",
  "items": {
    "type": "OBJECT",
    "properties": {
      "i": {"type": "INTEGER"},
      "phone": {"type": "STRING"},
      "category": {"type": "STRING"},
      "name": {"type": "STRING"},
      "corporation": {"type": "STRING"},
      "address": {"type": "STRING"},
      "memo": {"type": "STRING"},
      "mail": {"type": "STRING"},
      "confidence": {"type": "NUMBER"}
    },
    "required": ["i", "confidence"]
  }
}`

const systemPrompt = `あなたは日本の営業リスト (CSV) のデータクレンジング担当です。
与えられた列ヘッダと行データから、CRM の顧客レコード用に以下のフィールドを抽出・正規化してください。

- corporation: 会社名。法人格は「株式会社」「有限会社」「合同会社」等の正式表記に統一 (㈱→株式会社)。店舗名・支店名が混ざっている場合は会社名に含めたまま整理する。会社名が見当たらなければ空文字。
- name: 担当者の氏名 (人名)。姓と名の間は全角スペース 1 つ。人名が見当たらなければ空文字。
- phone: 電話番号。半角数字とハイフン区切り (例 03-1234-5678, 090-1234-5678) に正規化。国番号 +81 は 0 始まりに直す。
- mail: メールアドレス。半角小文字に正規化。
- address: 住所。全角の番地表記は「1-2-3」のような半角ハイフン区切りに、都道府県から始まる表記に正規化。
- category: 業種・カテゴリらしき値があればそのまま (軽く表記統一のみ)。
- memo: 上記に分類できない有用な情報があれば簡潔に。元の memo 列があれば保持。

重要なルール:
- 行データに根拠のない情報を創作しない。推測で補完できないフィールドは空文字にする。
- 各行につき JSON オブジェクトを 1 つ、入力の "i" (行番号) をそのまま返す。
- confidence はその行の抽出全体の確信度 (0.0-1.0)。
- 出力は指定された JSON schema に従うこと。`

// normalizeBatch は 1 バッチ分を generateContent に投げてパースする。
func (c *Client) normalizeBatch(ctx context.Context, headers []string, rows [][]string, targetFields []string, indexOffset int) ([]RowResult, error) {
	// 入力データを行番号付き JSON で渡す (プロンプトインジェクション面でも
	// 生 CSV 連結より安全寄り)。
	type inRow struct {
		I     int               `json:"i"`
		Cells map[string]string `json:"cells"`
	}
	inRows := make([]inRow, 0, len(rows))
	for ri, row := range rows {
		cells := make(map[string]string, len(headers))
		for hi, h := range headers {
			if hi < len(row) {
				v := row[hi]
				if len(v) > maxCellLen {
					// rune 境界を壊さないように切り詰める (日本語セル対策)
					r := []rune(v)
					if len(r) > maxCellLen {
						r = r[:maxCellLen]
					}
					v = string(r)
				}
				cells[h] = v
			}
		}
		inRows = append(inRows, inRow{I: indexOffset + ri, Cells: cells})
	}
	inJSON, err := json.Marshal(inRows)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal input: %w", err)
	}
	tfJSON, _ := json.Marshal(targetFields)

	userText := fmt.Sprintf(
		"対象フィールド: %s\n以下の行データを正規化してください。\n%s",
		string(tfJSON), string(inJSON),
	)

	reqBody := genRequest{
		SystemInstr: &genContent{Parts: []genPart{{Text: systemPrompt}}},
		Contents: []genContent{
			{Role: "user", Parts: []genPart{{Text: userText}}},
		},
		GenerationConfig: genGenConfig{
			Temperature:      0,
			ResponseMimeType: "application/json",
			ResponseSchema:   json.RawMessage(responseSchema),
			MaxOutputTokens:  8192,
		},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent", c.apiBase, c.model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("gemini: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// API キーはヘッダで渡す (URL クエリだとログに漏れやすい)。
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("gemini: read response: %w", err)
	}

	var gr genResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return nil, fmt.Errorf("gemini: parse response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := "unknown error"
		if gr.Error != nil {
			msg = gr.Error.Message
		}
		return nil, fmt.Errorf("gemini: API error (status %d): %s", resp.StatusCode, msg)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini: empty candidates in response")
	}

	var outRows []rowOut
	text := gr.Candidates[0].Content.Parts[0].Text
	if err := json.Unmarshal([]byte(text), &outRows); err != nil {
		return nil, fmt.Errorf("gemini: parse structured output: %w", err)
	}

	results := make([]RowResult, 0, len(outRows))
	target := make(map[string]bool, len(targetFields))
	for _, f := range targetFields {
		target[f] = true
	}
	for _, r := range outRows {
		if r.I < indexOffset || r.I >= indexOffset+len(rows) {
			continue // モデルが行番号を捏造した場合は捨てる
		}
		fields := map[string]string{}
		put := func(k, v string) {
			if target[k] && v != "" {
				fields[k] = v
			}
		}
		put("phone", r.Phone)
		put("category", r.Category)
		put("name", r.Name)
		put("corporation", r.Corporation)
		put("address", r.Address)
		put("memo", r.Memo)
		put("mail", r.Mail)
		conf := r.Confidence
		if conf < 0 {
			conf = 0
		}
		if conf > 1 {
			conf = 1
		}
		results = append(results, RowResult{Index: r.I, Fields: fields, Confidence: conf})
	}
	return results, nil
}
