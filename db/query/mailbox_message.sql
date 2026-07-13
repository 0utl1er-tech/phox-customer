-- name: CreateMailboxMessage :one
INSERT INTO "MailboxMessage" (
  id, mailbox_id, folder, message_id, from_addr, to_addrs, cc_addrs,
  subject, body_text, attachment_names, customer_id, occurred_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: GetMailboxMessage :one
SELECT * FROM "MailboxMessage" WHERE id = $1;

-- name: ListMailboxMessages :many
-- 本文は返さない (一覧用メタデータ)。has_attachments 判定用に attachment_names は返す。
SELECT id, mailbox_id, folder, message_id, from_addr, to_addrs, cc_addrs,
       subject, attachment_names, customer_id, occurred_at, created_at
FROM "MailboxMessage"
WHERE mailbox_id = $1
  AND (sqlc.narg('folder')::varchar IS NULL OR folder = sqlc.narg('folder'))
ORDER BY occurred_at DESC
LIMIT $2 OFFSET $3;

-- name: CountMailboxMessages :one
SELECT count(*) FROM "MailboxMessage"
WHERE mailbox_id = $1
  AND (sqlc.narg('folder')::varchar IS NULL OR folder = sqlc.narg('folder'));

-- name: SetMailboxSyncedAt :exec
UPDATE "Mailbox" SET synced_at = now(), updated_at = now() WHERE id = $1;
