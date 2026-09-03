-- name: CreateCompany :one
INSERT INTO "Company" (id, name)
VALUES ($1, $2)
RETURNING *;

-- name: GetCompany :one
SELECT * FROM "Company"
WHERE id = $1;

-- name: ListCompanies :many
SELECT * FROM "Company";

-- name: UpdateCompany :one
UPDATE "Company"
SET 
  name = COALESCE(sqlc.narg(name), name),
  updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteCompany :exec
DELETE FROM "Company" WHERE id = $1;

-- Phase 27f: 通話記録モード (管理者設定)

-- name: GetCompanySettings :one
SELECT id, call_log_mode, notify_webhook_url, notify_events FROM "Company"
WHERE id = $1;

-- name: UpdateCompanyCallLogMode :one
UPDATE "Company"
SET
  call_log_mode = $2,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING id, call_log_mode;
-- Phase 27h: キャンペーン反響通知 (管理者設定)

-- name: UpdateCompanyNotifySettings :one
-- narg = NULL は「変更しない」(部分更新)。
UPDATE "Company"
SET
  notify_webhook_url = COALESCE(sqlc.narg(notify_webhook_url), notify_webhook_url),
  notify_events = COALESCE(sqlc.narg(notify_events), notify_events),
  updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING id, notify_webhook_url, notify_events;
