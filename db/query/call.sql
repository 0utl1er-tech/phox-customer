-- name: CreateCall :one
INSERT INTO "Call" (id, customer_id, phone, user_id, status_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetCall :one
SELECT * FROM "Call" 
WHERE id = $1;

-- name: ListCallsByCustomerID :many
SELECT 
  "Call".id,
  "Call".customer_id,
  "Call".phone,
  "Call".user_id,
  "Call".status_id,
  "Call".created_at,
  "Call".updated_at,
  "User".name as user_name,
  "Status".name as status_name,
  "Status".priority as status_priority,
  "Status".effective as status_effective,
  "Status".ng as status_ng
FROM "Call" 
JOIN "User" ON "Call".user_id = "User".id
JOIN "Status" ON "Call".status_id = "Status".id
WHERE "Call".customer_id = $1
ORDER BY "Call".created_at DESC;

-- name: ListCallsByBookID :many
SELECT 
  "Call".id,
  "Call".customer_id,
  "Call".phone,
  "Call".user_id,
  "Call".status_id,
  "Call".created_at,
  "Call".updated_at,
  "User".name as user_name,
  "Status".name as status_name,
  "Customer".name as customer_name,
  "Customer".corporation as customer_corporation
FROM "Call" 
JOIN "User" ON "Call".user_id = "User".id
JOIN "Status" ON "Call".status_id = "Status".id
JOIN "Customer" ON "Call".customer_id = "Customer".id
WHERE "Customer".book_id = $1
ORDER BY "Call".created_at DESC;

-- name: UpdateCall :one
UPDATE "Call" 
SET 
  status_id = COALESCE(sqlc.narg(status_id), status_id),
  user_id = COALESCE(sqlc.narg(user_id), user_id)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteCall :exec
DELETE FROM "Call" WHERE id = $1;