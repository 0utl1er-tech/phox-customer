-- name: CreateStatus :one
INSERT INTO "Status" (id, book_id, priority, name, effective, ng)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetStatus :one
SELECT * FROM "Status"
WHERE id = $1;

-- name: ListStatusesByBookID :many
SELECT * FROM "Status"
WHERE book_id = $1
ORDER BY priority ASC;

-- name: UpdateStatus :one
UPDATE "Status"
SET 
  priority = COALESCE(sqlc.narg(priority), priority),
  name = COALESCE(sqlc.narg(name), name),
  effective = COALESCE(sqlc.narg(effective), effective),
  ng = COALESCE(sqlc.narg(ng), ng)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteStatus :exec
DELETE FROM "Status"
WHERE id = $1;

-- name: GetMaxStatusPriority :one
SELECT COALESCE(MAX(priority), 0) as max_priority FROM "Status"
WHERE book_id = $1;