-- name: CreateStaff :one
INSERT INTO "Staff" (id, name, sex)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetStaff :one
SELECT * FROM "Staff"
WHERE id = $1;

-- name: UpdateStaff :one
UPDATE "Staff"
SET 
  name = COALESCE(sqlc.narg(name), name),
  sex = COALESCE(sqlc.narg(sex), sex)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteStaff :exec
DELETE FROM "Staff"
WHERE id = $1;