-- Phase 27: キャンペーン (コールドメール一斉送信)

-- name: CreateCampaign :one
INSERT INTO "Campaign" (
    id, company_id, created_by, name, subject, body,
    track_opens, track_clicks,
    send_start_hour, send_end_hour, send_days,
    daily_cap_per_mailbox, min_interval_sec, warmup_enabled,
    sender_org, sender_address, sender_contact
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
)
RETURNING *;

-- name: GetCampaign :one
SELECT * FROM "Campaign" WHERE id = $1;

-- name: ListCampaignsByCompany :many
SELECT * FROM "Campaign"
WHERE company_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountCampaignsByCompany :one
SELECT count(*) FROM "Campaign" WHERE company_id = $1;

-- name: UpdateCampaignDraft :one
-- draft / paused のみ編集可 (状態チェックは service 層)。
UPDATE "Campaign"
SET
  name                  = COALESCE(sqlc.narg(name), name),
  subject               = COALESCE(sqlc.narg(subject), subject),
  body                  = COALESCE(sqlc.narg(body), body),
  track_opens           = COALESCE(sqlc.narg(track_opens), track_opens),
  track_clicks          = COALESCE(sqlc.narg(track_clicks), track_clicks),
  send_start_hour       = COALESCE(sqlc.narg(send_start_hour), send_start_hour),
  send_end_hour         = COALESCE(sqlc.narg(send_end_hour), send_end_hour),
  send_days             = COALESCE(sqlc.narg(send_days), send_days),
  daily_cap_per_mailbox = COALESCE(sqlc.narg(daily_cap_per_mailbox), daily_cap_per_mailbox),
  min_interval_sec      = COALESCE(sqlc.narg(min_interval_sec), min_interval_sec),
  warmup_enabled        = COALESCE(sqlc.narg(warmup_enabled), warmup_enabled),
  sender_org            = COALESCE(sqlc.narg(sender_org), sender_org),
  sender_address        = COALESCE(sqlc.narg(sender_address), sender_address),
  sender_contact        = COALESCE(sqlc.narg(sender_contact), sender_contact),
  updated_at            = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetCampaignStatus :one
-- 注意: $2 を複数箇所で使うと 42P08 (型推論の衝突) になるため CASE 側は
-- 明示キャスト必須 (実際に staging で 500 になった実績あり)。
UPDATE "Campaign"
SET status = $2,
    started_at   = CASE WHEN $2::varchar = 'running' AND started_at IS NULL THEN now() ELSE started_at END,
    completed_at = CASE WHEN $2::varchar IN ('completed', 'cancelled') THEN now() ELSE completed_at END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteCampaign :exec
DELETE FROM "Campaign" WHERE id = $1;

-- name: ListRunningCampaigns :many
-- worker の tick 対象。
SELECT * FROM "Campaign"
WHERE status = 'running'
ORDER BY created_at ASC;

-- ─── CampaignMailbox ─────────────────────────────────────────────

-- name: AddCampaignMailbox :exec
INSERT INTO "CampaignMailbox" (campaign_id, mailbox_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: DeleteCampaignMailboxes :exec
DELETE FROM "CampaignMailbox" WHERE campaign_id = $1;

-- name: ListCampaignMailboxes :many
-- worker が送信資格情報ごと引く (password_enc 込み)。
SELECT m.* FROM "Mailbox" m
JOIN "CampaignMailbox" cm ON cm.mailbox_id = m.id
WHERE cm.campaign_id = $1 AND m.active = true
ORDER BY m.created_at ASC;

-- ─── CampaignRecipient ───────────────────────────────────────────

-- name: CreateCampaignRecipients :copyfrom
INSERT INTO "CampaignRecipient" (
    id, campaign_id, customer_id, email, status, error
) VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListCampaignRecipients :many
-- status フィルタ (narg) + ページング。顧客名も返す。
SELECT r.*, c.name AS customer_name, c.corporation AS customer_corporation
FROM "CampaignRecipient" r
JOIN "Customer" c ON c.id = r.customer_id
WHERE r.campaign_id = sqlc.arg(campaign_id)
  AND (sqlc.narg(status)::varchar IS NULL OR r.status = sqlc.narg(status))
ORDER BY r.created_at ASC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountCampaignRecipients :one
SELECT count(*) FROM "CampaignRecipient"
WHERE campaign_id = sqlc.arg(campaign_id)
  AND (sqlc.narg(status)::varchar IS NULL OR status = sqlc.narg(status));

-- name: GetCampaignRecipient :one
SELECT * FROM "CampaignRecipient" WHERE id = $1;

-- name: GetCampaignRecipientByMessageID :one
SELECT * FROM "CampaignRecipient" WHERE message_id = $1;

-- name: ClaimCampaignRecipient :one
-- worker の per-recipient CAS claim。Redis ロックが破れても FOR UPDATE
-- SKIP LOCKED で at-most-once を担保する二重防御。
UPDATE "CampaignRecipient"
SET status = 'sending', locked_at = now(), mailbox_id = $2
WHERE id = (
  SELECT cr.id FROM "CampaignRecipient" cr
  WHERE cr.campaign_id = $1 AND cr.status = 'queued'
    AND (cr.next_step_at IS NULL OR cr.next_step_at <= now())
    AND cr.replied_at IS NULL AND cr.unsubscribed_at IS NULL AND cr.bounced_at IS NULL
  ORDER BY cr.attempts ASC, cr.created_at ASC
  LIMIT 1
  FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkRecipientSent :exec
UPDATE "CampaignRecipient"
SET status = 'sent', sent_at = now(), message_id = $2, error = '', locked_at = NULL
WHERE id = $1;

-- name: MarkRecipientFailed :exec
UPDATE "CampaignRecipient"
SET status = 'failed', error = $2, locked_at = NULL
WHERE id = $1;

-- name: MarkRecipientSkipped :exec
UPDATE "CampaignRecipient"
SET status = 'skipped', error = $2, locked_at = NULL
WHERE id = $1;

-- name: RequeueRecipient :exec
-- transient エラー (SMTP 4xx / dial 失敗)。attempts を増やして queued に戻す。
UPDATE "CampaignRecipient"
SET status = 'queued', attempts = attempts + 1, error = $2, locked_at = NULL, mailbox_id = NULL
WHERE id = $1;

-- name: RequeueFailedRecipients :execrows
-- 「失敗分を再キュー」ボタン (27c)。明示操作のみ。attempts はリセットする。
UPDATE "CampaignRecipient"
SET status = 'queued', attempts = 0, error = '', locked_at = NULL
WHERE campaign_id = $1 AND status = 'failed';

-- name: FailStaleSendingRecipients :execrows
-- janitor: claim したまま落ちた行。自動再送はしない (二重送信の方が害)。
UPDATE "CampaignRecipient"
SET status = 'failed', error = 'stale-claim: worker crashed mid-send', locked_at = NULL
WHERE status = 'sending' AND locked_at < $1;

-- name: CountQueuedRecipients :one
SELECT count(*) FROM "CampaignRecipient"
WHERE campaign_id = $1 AND status IN ('queued', 'sending');

-- name: CountSentSinceByMailbox :one
-- mailbox 毎の daily cap 判定 (全キャンペーン横断)。$2 = JST の当日 0 時。
SELECT count(*) FROM "CampaignRecipient"
WHERE mailbox_id = $1 AND sent_at >= $2;

-- name: MarkRecipientUnsubscribed :one
-- 冪等: 未設定のときだけ時刻を打つ。
UPDATE "CampaignRecipient"
SET unsubscribed_at = COALESCE(unsubscribed_at, now())
WHERE id = $1
RETURNING *;

-- name: MarkRecipientReplied :exec
UPDATE "CampaignRecipient"
SET replied_at = COALESCE(replied_at, now())
WHERE id = $1;

-- name: MarkRecipientBounced :exec
UPDATE "CampaignRecipient"
SET bounced_at = COALESCE(bounced_at, now())
WHERE id = $1;

-- name: MarkRecipientOpened :exec
UPDATE "CampaignRecipient"
SET first_opened_at = COALESCE(first_opened_at, now())
WHERE id = $1;

-- name: MarkRecipientClicked :exec
UPDATE "CampaignRecipient"
SET first_clicked_at = COALESCE(first_clicked_at, now())
WHERE id = $1;

-- name: GetCampaignStats :one
-- 非正規化時刻列の FILTER 集計 1 スキャンで済ませる (CampaignEvent 不要)。
SELECT
  count(*)                                          AS total,
  count(*) FILTER (WHERE status IN ('queued','sending') AND sent_at IS NULL) AS queued,
  count(*) FILTER (WHERE sent_at IS NOT NULL)       AS sent,
  count(*) FILTER (WHERE status = 'queued' AND sent_at IS NOT NULL) AS waiting_followup,
  count(*) FILTER (WHERE status = 'failed')         AS failed,
  count(*) FILTER (WHERE status = 'skipped')        AS skipped,
  count(*) FILTER (WHERE first_opened_at IS NOT NULL)  AS opened,
  count(*) FILTER (WHERE first_clicked_at IS NOT NULL) AS clicked,
  count(*) FILTER (WHERE replied_at IS NOT NULL)    AS replied,
  count(*) FILTER (WHERE bounced_at IS NOT NULL)    AS bounced,
  count(*) FILTER (WHERE unsubscribed_at IS NOT NULL)  AS unsubscribed,
  max(sent_at) AS last_sent_at
FROM "CampaignRecipient"
WHERE campaign_id = $1;

-- name: GetCustomersByIDs :many
-- スナップショット作成用。book_id は RBAC チェックに使う。
SELECT id, book_id, name, corporation, mail, phone, address, category
FROM "Customer"
WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: FindSentRecipientByFromAddr :many
-- 返信検出のフォールバック (27c): ヘッダで紐付かない「新規メールでの返信」を
-- from アドレスで直近の sent 受信者に当てる。
SELECT r.* FROM "CampaignRecipient" r
WHERE r.email = $1 AND r.status = 'sent' AND r.sent_at > $2
ORDER BY r.sent_at DESC;

-- ─── CampaignLink (27b) ─────────────────────────────────────────

-- name: CreateCampaignLink :exec
INSERT INTO "CampaignLink" (campaign_id, idx, url)
VALUES ($1, $2, $3)
ON CONFLICT (campaign_id, idx) DO UPDATE SET url = EXCLUDED.url;

-- name: GetCampaignLink :one
SELECT * FROM "CampaignLink" WHERE campaign_id = $1 AND idx = $2;

-- name: DeleteCampaignLinks :exec
DELETE FROM "CampaignLink" WHERE campaign_id = $1;

-- ─── CampaignEvent ──────────────────────────────────────────────

-- name: CreateCampaignEvent :exec
INSERT INTO "CampaignEvent" (id, recipient_id, kind, url, user_agent)
VALUES ($1, $2, $3, $4, $5);

-- name: ListCampaignEventsByRecipient :many
SELECT * FROM "CampaignEvent"
WHERE recipient_id = $1
ORDER BY created_at ASC;

-- ─── Suppression ────────────────────────────────────────────────

-- name: CreateSuppression :exec
-- 冪等: 既に登録済みなら何もしない (先勝ち — 最初の理由を残す)。
INSERT INTO "Suppression" (id, company_id, email, reason, campaign_id, note)
VALUES ($1, $2, lower($3), $4, $5, $6)
ON CONFLICT (company_id, email) DO NOTHING;

-- name: DeleteSuppression :exec
DELETE FROM "Suppression" WHERE id = $1;

-- name: GetSuppression :one
SELECT * FROM "Suppression" WHERE id = $1;

-- name: GetSuppressionByEmail :one
SELECT * FROM "Suppression" WHERE company_id = $1 AND email = lower($2);

-- name: ListSuppressionsByCompany :many
SELECT * FROM "Suppression"
WHERE company_id = sqlc.arg(company_id)
  AND (sqlc.narg(search)::varchar IS NULL OR email LIKE '%' || lower(sqlc.narg(search)) || '%')
ORDER BY created_at DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountSuppressionsByCompany :one
SELECT count(*) FROM "Suppression"
WHERE company_id = sqlc.arg(company_id)
  AND (sqlc.narg(search)::varchar IS NULL OR email LIKE '%' || lower(sqlc.narg(search)) || '%');

-- name: ListSuppressedEmailsIn :many
-- スナップショット作成時の一括チェック。
SELECT email FROM "Suppression"
WHERE company_id = $1 AND email = ANY(sqlc.arg(emails)::varchar[]);

-- name: ListCampaignLinks :many
SELECT * FROM "CampaignLink" WHERE campaign_id = $1 ORDER BY idx ASC;

-- name: GetCampaignDailyStats :many
-- ダッシュボードの折れ線グラフ用 (JST 日次)。各指標は各時刻列の日付で集計。
SELECT t.day::date AS day,
  count(*) FILTER (WHERE t.kind = 'sent')         AS sent,
  count(*) FILTER (WHERE t.kind = 'opened')       AS opened,
  count(*) FILTER (WHERE t.kind = 'clicked')      AS clicked,
  count(*) FILTER (WHERE t.kind = 'replied')      AS replied,
  count(*) FILTER (WHERE t.kind = 'bounced')      AS bounced,
  count(*) FILTER (WHERE t.kind = 'unsubscribed') AS unsubscribed
FROM (
  SELECT (r1.sent_at AT TIME ZONE 'Asia/Tokyo')::date AS day, 'sent'::text AS kind
    FROM "CampaignRecipient" r1 WHERE r1.campaign_id = sqlc.arg(campaign_id) AND r1.sent_at IS NOT NULL
  UNION ALL
  SELECT (r2.first_opened_at AT TIME ZONE 'Asia/Tokyo')::date, 'opened'
    FROM "CampaignRecipient" r2 WHERE r2.campaign_id = sqlc.arg(campaign_id) AND r2.first_opened_at IS NOT NULL
  UNION ALL
  SELECT (r3.first_clicked_at AT TIME ZONE 'Asia/Tokyo')::date, 'clicked'
    FROM "CampaignRecipient" r3 WHERE r3.campaign_id = sqlc.arg(campaign_id) AND r3.first_clicked_at IS NOT NULL
  UNION ALL
  SELECT (r4.replied_at AT TIME ZONE 'Asia/Tokyo')::date, 'replied'
    FROM "CampaignRecipient" r4 WHERE r4.campaign_id = sqlc.arg(campaign_id) AND r4.replied_at IS NOT NULL
  UNION ALL
  SELECT (r5.bounced_at AT TIME ZONE 'Asia/Tokyo')::date, 'bounced'
    FROM "CampaignRecipient" r5 WHERE r5.campaign_id = sqlc.arg(campaign_id) AND r5.bounced_at IS NOT NULL
  UNION ALL
  SELECT (r6.unsubscribed_at AT TIME ZONE 'Asia/Tokyo')::date, 'unsubscribed'
    FROM "CampaignRecipient" r6 WHERE r6.campaign_id = sqlc.arg(campaign_id) AND r6.unsubscribed_at IS NOT NULL
) t
GROUP BY t.day
ORDER BY t.day ASC;

-- ─── CampaignStep (Phase 27e: フォローアップシーケンス) ─────────

-- name: CreateCampaignStep :exec
INSERT INTO "CampaignStep" (campaign_id, step_no, wait_days, subject, body)
VALUES ($1, $2, $3, $4, $5);

-- name: DeleteCampaignSteps :exec
DELETE FROM "CampaignStep" WHERE campaign_id = $1;

-- name: ListCampaignSteps :many
SELECT * FROM "CampaignStep" WHERE campaign_id = $1 ORDER BY step_no ASC;

-- name: MarkRecipientStepSent :exec
-- step 送信完了。次ステップがある (next_step_at 非 NULL) 場合は queued に
-- 戻して待機、無ければ sent で確定。narg を複数箇所で使うため全て明示キャスト
-- (42P08 対策 — SetCampaignStatus で実績のある罠)。
UPDATE "CampaignRecipient"
SET status = CASE WHEN sqlc.narg(next_step_at)::timestamptz IS NULL THEN 'sent' ELSE 'queued' END,
    sent_at = now(),
    message_id = sqlc.arg(message_id),
    error = '',
    locked_at = NULL,
    current_step = sqlc.arg(current_step),
    next_step_at = sqlc.narg(next_step_at)::timestamptz,
    first_message_id = COALESCE(first_message_id, sqlc.arg(message_id))
WHERE id = sqlc.arg(id);

-- name: FinalizeStoppedFollowups :execrows
-- 返信/配信停止/バウンスが付いた「フォローアップ待ち」受信者のシーケンスを
-- 終了する (status='sent' で確定)。worker が tick 毎に呼ぶ。
UPDATE "CampaignRecipient"
SET status = 'sent', next_step_at = NULL, locked_at = NULL
WHERE campaign_id = $1 AND status = 'queued' AND sent_at IS NOT NULL
  AND (replied_at IS NOT NULL OR unsubscribed_at IS NOT NULL OR bounced_at IS NOT NULL);
