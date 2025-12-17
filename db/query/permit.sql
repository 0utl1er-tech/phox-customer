-- name: CreatePermit :one
INSERT INTO "Permit" (
    id, book_id, role, user_id
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: GetPermit :one
SELECT * FROM "Permit" 
WHERE id = $1;

-- name: ListPermits :many
SELECT * FROM "Permit" 
WHERE book_id = $1
ORDER BY created_at DESC;

-- name: UpdatePermit :one
UPDATE "Permit" 
SET 
  role = COALESCE(sqlc.narg(role), role),
  user_id = COALESCE(sqlc.narg(user_id), user_id)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeletePermit :exec
DELETE FROM "Permit" WHERE id = $1;

-- name: GetPermitsByUserID :many
SELECT * FROM "Permit" 
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetPermitByBookIDAndUserID :one
SELECT * FROM "Permit" 
WHERE book_id = $1 AND user_id = $2;

-- name: CheckUserAccessToBook :one
SELECT EXISTS(
    SELECT 1 FROM "Permit" 
    WHERE book_id = $1 AND user_id = $2
) as has_access;

-- name: CheckUserRoleForBook :one
SELECT role FROM "Permit" 
WHERE book_id = $1 AND user_id = $2;

-- name: ListPermitsWithUserInfo :many
SELECT p.id, p.book_id, p.user_id, p.role, u.name as user_name
FROM "Permit" p
JOIN "User" u ON p.user_id = u.id
WHERE p.book_id = $1
ORDER BY p.created_at DESC;
