-- name: CreateMailTemplate :one
INSERT INTO "MailTemplate" (
    id, book_id, name, subject, body
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: GetMailTemplate :one
SELECT * FROM "MailTemplate" WHERE id = $1;

-- name: ListMailTemplatesByBook :many
SELECT * FROM "MailTemplate"
WHERE book_id = $1
ORDER BY created_at DESC;

-- name: UpdateMailTemplate :one
UPDATE "MailTemplate"
SET
    name = $2,
    subject = $3,
    body = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteMailTemplate :exec
DELETE FROM "MailTemplate" WHERE id = $1;
