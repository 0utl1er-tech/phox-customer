-- name: CreateCustomer :one
INSERT INTO "Customer" (
    id, book_id, category, name, corporation, address, memo
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: GetCustomer :one
SELECT 
"Customer".id,
"Customer".book_id,
"Customer".category,
"Customer".name,
"Customer".corporation,
"Customer".address,
"Customer".memo,
"Customer".pic,
"Customer".leader,
"Customer".updated_at,
"Customer".created_at,
pic."id" as pic_id,
pic."name" as pic_name,
pic."sex" as pic_sex,
leader."id" as leader_id,
leader."name" as leader_name,
leader."sex" as leader_sex
FROM "Customer"
JOIN "Staff" AS pic ON "Customer".pic = "Staff".id
JOIN "Staff" AS leader ON "Customer".leader = "Staff".id
WHERE book_id = $1;

-- name: ListCustomers :many
SELECT 
"Customer".id,
"Customer".book_id,
"Customer".category,
"Customer".name,
"Customer".corporation,
"Customer".address,
"Customer".memo,
"Customer".pic,
"Customer".leader,
"Customer".updated_at,
"Customer".created_at,
pic."id" as pic_id,
pic."name" as pic_name,
pic."sex" as pic_sex,
leader."id" as leader_id,
leader."name" as leader_name,
leader."sex" as leader_sex
FROM "Customer" 
JOIN "Staff" AS pic ON "Customer".pic = "Staff".id
JOIN "Staff" AS leader ON "Customer".leader = "Staff".id
WHERE book_id = $1 
ORDER BY "Customer"."updated_at" DESC
LIMIT $2
OFFSET $3;

-- name: UpdateCustomer :one
UPDATE "Customer" 
SET 
  category = COALESCE(sqlc.narg(category), category),
  name = COALESCE(sqlc.narg(name), name),
  corporation = COALESCE(sqlc.narg(corporation), corporation),
  address = COALESCE(sqlc.narg(address), address),
  memo = COALESCE(sqlc.narg(memo), memo)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteCustomer :exec
DELETE FROM "Customer" WHERE id = $1;
