-- name: CreateStatus :one
INSERT INTO "Status" (id, book_id, priority, name, effective, ng)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetStatus :one
SELECT * FROM "Status"
WHERE id = $1;

-- name: ListStatuses :many
SELECT * FROM "Status";

-- name: UpdateStatus :one
UPDATE "Status"
SET 
  book_id = COALESCE(sqlc.narg(book_id), book_id),
  priority = COALESCE(sqlc.narg(priority), priority),
  name = COALESCE(sqlc.narg(name), name),
  effective = COALESCE(sqlc.narg(effective), effective),
  ng = COALESCE(sqlc.narg(ng), ng)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteStatus :exec
DELETE FROM "Status"
WHERE id = $1;