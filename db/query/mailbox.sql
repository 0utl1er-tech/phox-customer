-- name: CreateMailbox :one
INSERT INTO "Mailbox" (
    id, company_id, address, display_name, smtp_username, password_enc, active
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetMailbox :one
SELECT * FROM "Mailbox" WHERE id = $1;

-- name: UpdateMailbox :one
-- password_enc は narg。nil を渡せば既存パスワードを維持する。
UPDATE "Mailbox"
SET
  display_name  = COALESCE(sqlc.narg(display_name), display_name),
  smtp_username = COALESCE(sqlc.narg(smtp_username), smtp_username),
  password_enc  = COALESCE(sqlc.narg(password_enc), password_enc),
  active        = COALESCE(sqlc.narg(active), active),
  updated_at    = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteMailbox :exec
DELETE FROM "Mailbox" WHERE id = $1;

-- name: ListMailboxesByUserID :many
-- 呼び出しユーザーが MailboxPermit を持つメールボックス一覧 (自分のロール付き)。
SELECT m.id, m.company_id, m.address, m.display_name, m.smtp_username,
       m.active, m.created_at, m.updated_at, p.role
FROM "Mailbox" m
JOIN "MailboxPermit" p ON m.id = p.mailbox_id
WHERE p.user_id = $1
ORDER BY m.created_at DESC;

-- name: ListActiveMailboxesByCompany :many
-- IMAP worker が polling 対象を DB から引くためのクエリ (password_enc 込み)。
SELECT * FROM "Mailbox"
WHERE company_id = $1 AND active = true
ORDER BY created_at ASC;

-- name: ListAllActiveMailboxes :many
-- 全 company の active メールボックス (単一テナント運用では company フィルタ不要)。
SELECT * FROM "Mailbox"
WHERE active = true
ORDER BY created_at ASC;

-- name: GetMailboxPermitByMailboxIDAndUserID :one
SELECT * FROM "MailboxPermit"
WHERE mailbox_id = $1 AND user_id = $2;

-- name: CheckUserRoleForMailbox :one
SELECT role FROM "MailboxPermit"
WHERE mailbox_id = $1 AND user_id = $2;

-- name: CreateMailboxPermit :one
INSERT INTO "MailboxPermit" (
    id, mailbox_id, user_id, role
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: UpdateMailboxPermitRole :one
UPDATE "MailboxPermit"
SET role = $2, updated_at = now()
WHERE mailbox_id = $1 AND user_id = $3
RETURNING *;

-- name: DeleteMailboxPermit :exec
DELETE FROM "MailboxPermit" WHERE mailbox_id = $1 AND user_id = $2;

-- name: ListMailboxPermitsWithUserInfo :many
SELECT p.id, p.mailbox_id, p.user_id, p.role, u.name as user_name
FROM "MailboxPermit" p
JOIN "User" u ON p.user_id = u.id
WHERE p.mailbox_id = $1
ORDER BY p.created_at DESC;
