-- name: CreateUser :one
INSERT INTO "User" (id, company_id, name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUser :one
SELECT * FROM "User"
WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM "User";

-- name: UpdateUser :one
UPDATE "User"
SET 
  company_id = COALESCE(sqlc.narg(company_id), company_id),
  name = COALESCE(sqlc.narg(name), name),
  updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteUser :one
DELETE FROM "User"
WHERE id = $1
RETURNING *;