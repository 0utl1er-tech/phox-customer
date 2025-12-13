-- name: GetContact :one
SELECT * FROM "Contact" 
WHERE id = $1;

-- name: ListContacts :many
SELECT * FROM "Contact" 
WHERE customer_id = $1;

-- name: CreateContact :one
INSERT INTO "Contact" (id, customer_id, name, sex, phone, mail, fax)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateContact :one
UPDATE "Contact" 
SET 
  name = COALESCE(sqlc.narg(name), name),
  sex = COALESCE(sqlc.narg(sex), sex),
  phone = COALESCE(sqlc.narg(phone), phone),
  mail = COALESCE(sqlc.narg(mail), mail),
  fax = COALESCE(sqlc.narg(fax), fax),
  updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteContact :exec
DELETE FROM "Contact" 
WHERE id = $1;

-- name: ListAllContacts :many
SELECT 
  "Contact".id, 
  "Contact".customer_id,
  "Contact".name,
  "Contact".sex,
  "Contact".phone,
  "Contact".mail,
  "Contact".fax,
  "Customer".name as customer_name,
  "Customer".corporation as customer_corporation
FROM "Contact" 
JOIN "Customer" ON "Contact".customer_id = "Customer".id
ORDER BY "Contact".created_at DESC
LIMIT $1 OFFSET $2;