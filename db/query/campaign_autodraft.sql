-- Phase 28f: キャンペーン自動下書きテンプレート (Book 名パターン毎)

-- name: CreateCampaignAutoDraft :one
INSERT INTO "CampaignAutoDraft" (
    id, company_id, name, enabled, book_name_pattern,
    subject, body, followups, mailbox_ids,
    track_opens, track_clicks,
    send_start_hour, send_end_hour, send_days,
    daily_cap_per_mailbox, min_interval_sec, warmup_enabled,
    bounce_pause_threshold,
    sender_org, sender_address, sender_contact,
    creator_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
    $15, $16, $17, $18, $19, $20, $21, $22
)
RETURNING *;

-- name: GetCampaignAutoDraft :one
SELECT * FROM "CampaignAutoDraft" WHERE id = $1;

-- name: ListCampaignAutoDraftsByCompany :many
SELECT * FROM "CampaignAutoDraft"
WHERE company_id = $1
ORDER BY created_at ASC;

-- name: ListEnabledCampaignAutoDrafts :many
-- worker の tick 対象 (全会社横断)。
SELECT * FROM "CampaignAutoDraft"
WHERE enabled
ORDER BY created_at ASC;

-- name: UpdateCampaignAutoDraft :one
-- narg = NULL は「変更しない」(部分更新)。
UPDATE "CampaignAutoDraft"
SET
  name                   = COALESCE(sqlc.narg(name), name),
  enabled                = COALESCE(sqlc.narg(enabled), enabled),
  book_name_pattern      = COALESCE(sqlc.narg(book_name_pattern), book_name_pattern),
  subject                = COALESCE(sqlc.narg(subject), subject),
  body                   = COALESCE(sqlc.narg(body), body),
  followups              = COALESCE(sqlc.narg(followups), followups),
  mailbox_ids            = COALESCE(sqlc.narg(mailbox_ids)::uuid[], mailbox_ids),
  track_opens            = COALESCE(sqlc.narg(track_opens), track_opens),
  track_clicks           = COALESCE(sqlc.narg(track_clicks), track_clicks),
  send_start_hour        = COALESCE(sqlc.narg(send_start_hour), send_start_hour),
  send_end_hour          = COALESCE(sqlc.narg(send_end_hour), send_end_hour),
  send_days              = COALESCE(sqlc.narg(send_days), send_days),
  daily_cap_per_mailbox  = COALESCE(sqlc.narg(daily_cap_per_mailbox), daily_cap_per_mailbox),
  min_interval_sec       = COALESCE(sqlc.narg(min_interval_sec), min_interval_sec),
  warmup_enabled         = COALESCE(sqlc.narg(warmup_enabled), warmup_enabled),
  bounce_pause_threshold = COALESCE(sqlc.narg(bounce_pause_threshold), bounce_pause_threshold),
  sender_org             = COALESCE(sqlc.narg(sender_org), sender_org),
  sender_address         = COALESCE(sqlc.narg(sender_address), sender_address),
  sender_contact         = COALESCE(sqlc.narg(sender_contact), sender_contact),
  updated_at             = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: TouchCampaignAutoDraftCreated :exec
-- 下書きを 1 本作った直後に呼ぶ (UI の「最終作成日」表示用)。
UPDATE "CampaignAutoDraft"
SET last_created_at = now(), updated_at = now()
WHERE id = $1;

-- name: DeleteCampaignAutoDraft :exec
DELETE FROM "CampaignAutoDraft" WHERE id = $1;

-- name: ListUnclaimedBooksByNamePattern :many
-- パターンに LIKE 一致し、かつ自動下書きが未作成 (Campaign.source_book_id に
-- 未登場) の Book。ESCAPE '\' なのでパターン側で `\_` とすればリテラル _。
-- 古い Book から順に処理し、1 tick あたりの件数を制限する。
SELECT b.* FROM "Book" b
WHERE b.name LIKE sqlc.arg(pattern)::varchar ESCAPE '\'
  AND NOT EXISTS (
    SELECT 1 FROM "Campaign" c WHERE c.source_book_id = b.id
  )
ORDER BY b.created_at ASC
LIMIT sqlc.arg(max_books);
