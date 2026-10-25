package campaign

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// 自動の差し込み値 offer_deadline (特典の期限)。
//
// 「その人に 1 通目を送った日 (JST) + N 日、土日祝なら次の平日」を 1 通目の
// 送信直前に 1 回だけ決め、顧客の custom_fields.offer_deadline に保存する。
// 2 通目以降は保存済みの値を {{fields.offer_deadline}} で差し込むだけで
// 再計算しない — 全通で同じ日付でなければならないため。
// キーはキャンペーンに結び付けない (同じ顧客が別キャンペーンにいれば先に
// 決まった値をそのまま使う)。
const (
	OfferDeadlineKey = "offer_deadline"

	// offerDeadlineDaysEnv で日数を変えられる (既定 21)。
	offerDeadlineDaysEnv     = "CAMPAIGN_OFFER_DEADLINE_DAYS"
	defaultOfferDeadlineDays = 21
)

// offerDeadlineDaysFromEnv は CAMPAIGN_OFFER_DEADLINE_DAYS を読む。未設定・
// 不正値 (数値でない / 0 以下) は既定の 21 日。
func offerDeadlineDaysFromEnv() int {
	raw := strings.TrimSpace(os.Getenv(offerDeadlineDaysEnv))
	if raw == "" {
		return defaultOfferDeadlineDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		log.Warn().Str("value", raw).Int("default", defaultOfferDeadlineDays).
			Msg("campaign: " + offerDeadlineDaysEnv + " が不正なので既定値を使う")
		return defaultOfferDeadlineDays
	}
	return n
}

// OfferDeadlineDate は sentAt の JST 暦日に days 日足し、土日・日本の祝日に
// 当たれば次の平日まで送る。戻り値は JST 0 時。
// covered=false は、判定した日のどこかが祝日表 (jpholiday.go) の範囲外の年に
// あった — その日は土日だけ飛ばしている (祝日かもしれない)。
func OfferDeadlineDate(sentAt time.Time, days int) (deadline time.Time, covered bool) {
	n := sentAt.In(jst)
	d := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, jst).AddDate(0, 0, days)
	covered = true
	for {
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			d = d.AddDate(0, 0, 1)
			continue
		}
		_, holiday, ok := jpHoliday(d)
		if !ok {
			covered = false
		}
		if holiday {
			d = d.AddDate(0, 0, 1)
			continue
		}
		return d, covered
	}
}

var jpWeekdays = [...]string{"日", "月", "火", "水", "木", "金", "土"}

// FormatJPMonthDayWeekday は「11月15日（土）」の形 (月日はゼロ埋め無し、
// 括弧は全角) で返す。
func FormatJPMonthDayWeekday(t time.Time) string {
	t = t.In(jst)
	return fmt.Sprintf("%d月%d日（%s）", int(t.Month()), t.Day(), jpWeekdays[t.Weekday()])
}

// campaignReferencesField はキャンペーン本体 (1 通目) かいずれかのステップの
// 件名・本文が {{fields.<key>}} を参照しているかを返す。
func campaignReferencesField(c db.Campaign, steps []db.CampaignStep, key string) bool {
	tpls := []string{c.Subject, c.Body}
	for _, s := range steps {
		tpls = append(tpls, s.Subject, s.Body)
	}
	for _, tpl := range tpls {
		for _, k := range ReferencedFieldKeys(tpl) {
			if k == key {
				return true
			}
		}
	}
	return false
}

// customFieldSetter は保存クエリだけを切り出したもの (テストで差し替える)。
type customFieldSetter interface {
	SetCustomerCustomFieldIfAbsent(ctx context.Context, arg db.SetCustomerCustomFieldIfAbsentParams) (string, error)
}

// ensureOfferDeadline は送信直前 (AddFieldVars の後) に呼ぶ。1 通目
// (stepToSend == 1) で、キャンペーンが {{fields.offer_deadline}} を使っていて、
// 顧客がまだ値を持っていないときだけ期限を決めて保存し、vars に入れる。
// それ以外は何もしない (既存値は AddFieldVars で vars に入っている)。
// 2 通目以降は再計算しない — 値が無ければ warning だけ出す (空文字で届く)。
//
// 保存に失敗しても送信は止めない — その回は計算値を差し込む。ただし 2 通目
// 以降は値が無いまま (= 空文字で差し込まれる) か、次に 1 通目を送る経路で
// 別の送信日から計算し直されてずれるので、warning で目立たせる。
func ensureOfferDeadline(
	ctx context.Context, q customFieldSetter,
	c db.Campaign, steps []db.CampaignStep, stepToSend int32,
	customerID uuid.UUID, vars map[string]string,
	now time.Time, days int,
) {
	if !campaignReferencesField(c, steps, OfferDeadlineKey) {
		return
	}
	varKey := FieldsPrefix + OfferDeadlineKey
	if vars[varKey] != "" {
		return // 保存済み — 上書きしない
	}
	if stepToSend != 1 {
		// 1 通目を機能の導入前に送った人など。ここで決めると「1 通目の送信日」
		// ではなくなるので決めない。
		log.Warn().
			Str("campaign", c.ID.String()).
			Str("customer", customerID.String()).
			Int32("step", stepToSend).
			Msg("campaign: offer_deadline が未保存のままフォローアップを送る — 期限は空文字で差し込まれる")
		return
	}
	d, covered := OfferDeadlineDate(now, days)
	value := FormatJPMonthDayWeekday(d)
	if !covered {
		log.Warn().
			Str("campaign", c.ID.String()).
			Str("customer", customerID.String()).
			Str("offer_deadline", value).
			Int("holiday_table_last_year", jpHolidayLastYear).
			Msg("campaign: offer_deadline が祝日表の範囲外 — 土日だけ飛ばした (jpholiday.go に年を足すこと)")
	}
	stored, err := q.SetCustomerCustomFieldIfAbsent(ctx, db.SetCustomerCustomFieldIfAbsentParams{
		Key: OfferDeadlineKey, Value: value, ID: customerID,
	})
	if err != nil || stored == "" {
		log.Warn().Err(err).
			Str("campaign", c.ID.String()).
			Str("customer", customerID.String()).
			Str("offer_deadline", value).
			Msg("campaign: offer_deadline の保存に失敗 — 今回は計算値を差し込む。2 通目以降と日付がずれるので顧客の custom_fields を手で直すこと")
		vars[varKey] = value
		return
	}
	// 競合で別の値が先に入っていた場合も、保存されている方に揃える。
	vars[varKey] = stored
}
