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