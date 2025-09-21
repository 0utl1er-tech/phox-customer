-- name: GetContact :one
SELECT * FROM "Contact" 
WHERE id = $1;

-- name: ListContacts :many
SELECT "Contact".id, 
  "Contact".customer_id,
  "Contact".staff_id,
  "Contact".mail,
  "Contact".phone,
  "Contact".fax,
  staff.name as staff_name,
  staff.sex as staff_sex
FROM "Contact" 
JOIN "Staff" AS staff ON "Contact".staff_id = "Staff".id
WHERE customer_id = $1;

-- name: CreateContact :one
INSERT INTO "Contact" (id, customer_id, staff_id, mail, phone, fax)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateContact :one
UPDATE "Contact" 
SET 
  staff_id = COALESCE(sqlc.narg(staff_id), staff_id),
  mail = COALESCE(sqlc.narg(mail), mail),
  phone = COALESCE(sqlc.narg(phone), phone),
  fax = COALESCE(sqlc.narg(fax), fax)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteContact :exec
DELETE FROM "Contact" 
WHERE id = $1;