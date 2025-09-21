-- name: CreateCall :one
INSERT INTO "Call" (id, customer_id, contact_id, user_id, status_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetCall :one
SELECT * FROM "Call" 
WHERE id = $1;

-- name: ListCalls :many
SELECT "Call".id, 
  "Call".customer_id,
  "Call".contact_id, 
  "Call".user_id, 
  "Call".status_id,
  contact.id as contact_id,
  contact.customer_id as contact_customer_id,
  contact.phone as contact_phone,
  contact.mail as contact_mail,
  contact.fax as contact_fax,
  "User".id as user_id, 
  "User".name as user_name,
  "Status".id as status_id,
  "Status".priority as status_priority,
  "Status".name as status_name,
  "Status".effective as status_effective,
  "Status".ng as status_ng
FROM "Call" 
JOIN "Contact" AS contact ON "Call".contact_id = contact.id
JOIN "User" ON "Call".user_id = "User".id
JOIN "Status" ON "Call".status_id = "Status".id
WHERE "Call".customer_id = $1;

-- name: UpdateCall :one
UPDATE "Call" 
SET 
  status_id = COALESCE(sqlc.narg(status_id), status_id),
  user_id = COALESCE(sqlc.narg(user_id), user_id)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteCall :exec
DELETE FROM "Call" WHERE id = $1;