// Package customfields は Customer.custom_fields (JSONB) の正規化・上限・
// シリアライズを一箇所に集める。
//
// Phase 29b: 「顧客ごとの任意差し込み変数」。CSV の未知列・MCP・RPC から
// 入ってきた任意のキー/値を、キャンペーン本文の {{fields.<key>}} で安全に
// 差し込める形に落とす。
//
// 設計上の約束:
//   - キーは [a-z0-9_]{1,64}。それ以外の文字は '_' に潰す (小文字化・トリム後)。
//     テンプレート側の参照も同じ字種しか受け付けないので、書き手が
//     {{fields.<列名>}} と書けば必ず当たる。
//   - 値はプレーンテキスト。改行を含んでよい (箇条書きの診断結果が主用途)。
//     HTML パートでのエスケープと改行→<br> は campaign.BuildHTMLBody が行う。
//   - 件数・長さに上限を設ける。営業リストの CSV は素性が知れないので、
//     1 顧客あたりの JSONB が青天井に膨らまないようにする。
package customfields

import (
	"encoding/json"
	"sort"
	"strings"
)

const (
	// MaxFields は 1 顧客が持てるキーの数。超過分は捨てる。
	MaxFields = 32
	// MaxKeyLen はキーの最大文字数。
	MaxKeyLen = 64
	// MaxValueLen は値の最大バイト数。超過分は切り詰める。
	MaxValueLen = 4096
)

// NormalizeKey はヘッダ名などの任意文字列を差し込みキーに正規化する。
// 小文字化 → 前後トリム → [a-z0-9_] 以外を '_' に置換 → 64 文字に切り詰め。
// 有効な文字が 1 つも無い (= すべて '_' になる) 場合は ok=false。
func NormalizeKey(raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", false
	}
	var b strings.Builder
	meaningful := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			meaningful = true
		case r == '_':
			b.WriteRune('_')
		default:
			b.WriteRune('_')
		}
		if b.Len() >= MaxKeyLen {
			break
		}
	}
	if !meaningful {
		// 記号だけ / 日本語だけの列名は '_' の羅列にしかならず、テンプレート
		// から参照しようがないので取り込まない。
		return "", false
	}
	return b.String(), true
}

// ValidKey はテンプレート側の参照 ({{fields.<key>}}) が正規化済みキーの
// 字種を満たすかを返す。
func ValidKey(k string) bool {
	if k == "" || len(k) > MaxKeyLen {
		return false
	}
	for _, r := range k {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

// TruncateValue は値を MaxValueLen バイトに切り詰める (UTF-8 境界を壊さない)。
func TruncateValue(v string) string {
	if len(v) <= MaxValueLen {
		return v
	}
	cut := MaxValueLen
	for cut > 0 && !isUTF8Start(v[cut]) {
		cut--
	}
	return v[:cut]
}

func isUTF8Start(b byte) bool { return b&0xC0 != 0x80 }

// Sanitize はキーを正規化し、上限 (件数・値長) を適用した新しい map を返す。
// キーの衝突は「先に現れた方を残す」。件数超過はキー昇順で先頭 MaxFields 件。
func Sanitize(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	// map の反復順は不定なので、超過切り捨てが実行ごとにブレないよう
	// 正規化キーの昇順で決める。
	type kv struct{ k, v string }
	pairs := make([]kv, 0, len(in))
	for k, v := range in {
		nk, ok := NormalizeKey(k)
		if !ok {
			continue
		}
		pairs = append(pairs, kv{nk, TruncateValue(v)})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		if _, dup := out[p.k]; dup {
			continue
		}
		if len(out) >= MaxFields {
			break
		}
		out[p.k] = p.v
	}
	return out
}

// Marshal は JSONB 列に入れる []byte を返す。空/nil でも "{}" を返すので、
// NOT NULL 制約に対して常に安全。
func Marshal(m map[string]string) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

// MarshalSanitized は Sanitize してから Marshal する (書き込み経路の既定)。
func MarshalSanitized(m map[string]string) ([]byte, error) {
	return Marshal(Sanitize(m))
}

// Unmarshal は JSONB 列の値を map に戻す。壊れた JSON や想定外の形
// (ネスト・配列・数値) は「その顧客には差し込み変数が無い」として扱い、
// エラーにしない — 送信経路を JSON の不整合で止めないための degraded mode。
func Unmarshal(raw []byte) map[string]string {
	if len(raw) == 0 {
		return map[string]string{}
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return map[string]string{}
	}
	return m
}
