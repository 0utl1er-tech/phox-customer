package campaign

import (
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

// EstimateCompletion は残 queued 数とペーシング設定から完了予定時刻を見積もる。
// worker の実挙動 (JST 送信窓・曜日 bitmask・mailbox 毎日次上限・warmup ramp・
// 送信間隔) を日単位でシミュレーションする。ジッターや SMTP エラーは無視した
// 目安値。見積もれない場合 (mailbox 0 台・1 年経っても終わらない等) は nil。
//
// sentToday は「JST 当日すでに送った数」— 当日の残キャパ計算に使う
// (全 mailbox 合算の近似で十分)。
func EstimateCompletion(c db.Campaign, mailboxCount int, queued int64, sentToday int64, now time.Time) *time.Time {
	if queued <= 0 || mailboxCount == 0 {
		return nil
	}
	interval := float64(c.MinIntervalSec)
	if interval <= 0 {
		interval = 90
	}
	days := c.SendDays
	if days == 0 {
		days = 31 // 平日
	}
	startH, endH := int(c.SendStartHour), int(c.SendEndHour)
	if endH <= startH {
		return nil
	}
	windowSecs := float64((endH - startH) * 3600)

	w := &Worker{} // effectiveCap は状態を持たない
	remaining := queued
	nowJST := now.In(jst)

	// 当日分: 現在時刻から窓の終わりまでで送れる数 (日次上限の残りが上限)。
	day := time.Date(nowJST.Year(), nowJST.Month(), nowJST.Day(), 0, 0, 0, 0, jst)
	for i := 0; i < 400; i++ { // 上限 400 日 (超えたら「見積不能」)
		cur := day.AddDate(0, 0, i)
		bit := int32(1) << ((int(cur.Weekday()) + 6) % 7)
		if days&bit == 0 {
			continue
		}
		capPerMailbox := int64(w.effectiveCap(c, cur.Add(12*time.Hour)))
		dayCap := capPerMailbox * int64(mailboxCount)

		var availSecs float64
		windowStart := cur.Add(time.Duration(startH) * time.Hour)
		windowEnd := cur.Add(time.Duration(endH) * time.Hour)
		if i == 0 {
			if nowJST.After(windowEnd) {
				continue // 今日の窓は終わっている
			}
			from := nowJST
			if from.Before(windowStart) {
				from = windowStart
			}
			availSecs = windowEnd.Sub(from).Seconds()
			dayCap -= sentToday
			if dayCap < 0 {
				dayCap = 0
			}
		} else {
			availSecs = windowSecs
		}

		// worker は tick 毎に mailbox ローテーションで送る → 実効レートは
		// mailbox 台数 × (1 通 / interval)。
		throughput := int64(availSecs / interval * float64(mailboxCount))
		sendable := throughput
		if dayCap < sendable {
			sendable = dayCap
		}
		if sendable <= 0 {
			continue
		}
		if remaining <= sendable {
			// この日のうちに終わる。残数ぶんの所要秒を足して確定。
			var from time.Time
			if i == 0 {
				from = nowJST
				if from.Before(windowStart) {
					from = windowStart
				}
			} else {
				from = windowStart
			}
			secsNeeded := float64(remaining) * interval / float64(mailboxCount)
			eta := from.Add(time.Duration(secsNeeded) * time.Second)
			if eta.After(windowEnd) {
				eta = windowEnd
			}
			return &eta
		}
		remaining -= sendable
	}
	return nil // 400 日で終わらない見積り
}
