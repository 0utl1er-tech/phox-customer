package zoom

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// ErrNoMatch は phone に該当する Customer が見つからなかった事を表す。
// webhook handler はこの error 時 Activity を作らず skip ログを出す。
var ErrNoMatch = errors.New("zoom: no customer matches phone number")

// CustomerMatch は phone マッチ結果。複数候補があった場合の disambiguation
// 後の確定 1 件。
type CustomerMatch struct {
	CustomerID  uuid.UUID
	ContactID   uuid.UUID // uuid.Nil if matched on Customer.phone (not via Contact)
	BookID      uuid.UUID
	Name        string
	MatchSource string // "customer" | "contact"
	// LastActivityAt は matching 時に参照した最新 Activity 時刻 (debug log 用)。
	LastActivityAt time.Time
}

// PhoneToDigits は phone 文字列から数字のみ抽出 → 末尾 10 桁を返す。
// DB 側の SQL `right(regexp_replace(...), 10)` と完全に同じ正規化を Go 側で
// 行うことで、入出力 1:1 一致を保証する。
//
// 例:
//
//	"09037241917"      → "9037241917"
//	"+819037241917"    → "9037241917"
//	"090-3724-1917"    → "9037241917"
//	"(03) 1234-5678"   → "0312345678"
//	""                 → ""
//	"1234"             → "" (10 桁未満は無効と扱う)
func PhoneToDigits(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) < 10 {
		return ""
	}
	return d[len(d)-10:]
}

// MatchCustomerByPhoneAndTime は rawPhone に紐づく Customer を 1 件返す。
//
// マッチング戦略:
//  1. phone を末尾 10 桁の digit string に正規化
//  2. FindCustomersByPhoneDigits で全候補取得
//  3. 候補 0 → ErrNoMatch
//  4. 候補 1 → そのまま返却
//  5. 候補 ≥ 2 → 各候補について callTime より前の最新 Activity を引き、
//     occurred_at が callTime に最も近い (= 最新の) 候補を採用。
//     全候補に Activity が無ければ先頭候補にフォールバック。
//
// lookback は「Activity が古すぎる場合は無視」の閾値。例えば 30d を渡すと、
// 30 日以上前の Activity しか持たない候補は disambiguation 対象外になる
// (マッチを見送るのではなく、その候補を選ばない、というニュアンス)。
func MatchCustomerByPhoneAndTime(
	ctx context.Context,
	queries *db.Queries,
	rawPhone string,
	callTime time.Time,
	lookback time.Duration,
) (*CustomerMatch, error) {
	digits := PhoneToDigits(rawPhone)
	if digits == "" {
		return nil, ErrNoMatch
	}
	rows, err := queries.FindCustomersByPhoneDigits(ctx, digits)
	if err != nil {
		return nil, fmt.Errorf("zoom match: query: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNoMatch
	}

	// Helper: row → CustomerMatch (Activity 時刻は外で詰める)
	toMatch := func(r db.FindCustomersByPhoneDigitsRow) *CustomerMatch {
		m := &CustomerMatch{
			CustomerID:  r.CustomerID,
			BookID:      r.BookID,
			Name:        r.Name,
			MatchSource: r.Source,
		}
		if r.ContactID.Valid {
			m.ContactID = r.ContactID.Bytes
		}
		return m
	}

	if len(rows) == 1 {
		return toMatch(rows[0]), nil
	}

	// 複数候補 — occurred_at 最近接で選ぶ
	earliestAcceptable := callTime.Add(-lookback)
	var best *CustomerMatch
	var bestTime time.Time
	for _, r := range rows {
		latest, lerr := queries.GetMostRecentActivityForCustomer(
			ctx,
			db.GetMostRecentActivityForCustomerParams{
				CustomerID: r.CustomerID,
				Before:     callTime,
			},
		)
		if lerr != nil {
			// Activity 0 件 (pgx.ErrNoRows) や転送エラー — その候補は無視
			continue
		}
		if latest.OccurredAt.Before(earliestAcceptable) {
			continue
		}
		if best == nil || latest.OccurredAt.After(bestTime) {
			best = toMatch(r)
			best.LastActivityAt = latest.OccurredAt
			bestTime = latest.OccurredAt
		}
	}
	if best != nil {
		return best, nil
	}

	// 全候補に最近 Activity 無し — 先頭にフォールバック (任意性が残るが、
	// 「全く取り込まない」よりは「とりあえず最初の候補に紐付ける」の方が
	// 業務的にレビュー可能で実用的)。
	log.Debug().
		Str("phone_digits", digits).
		Int("candidates", len(rows)).
		Msg("zoom match: no candidate has recent activity, falling back to first")
	return toMatch(rows[0]), nil
}
