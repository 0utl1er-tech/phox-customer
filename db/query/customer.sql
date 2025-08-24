-- name: CreateCustomer :one
INSERT INTO "Customer" (
    id, book_id, category_id, name, corporation, address, memo
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: GetCustomer :one
SELECT * FROM "Customer" WHERE id = $1;

-- name: ListCustomers :many
SELECT * FROM "Customer" 
WHERE book_id = $1 
ORDER BY created_at DESC;

-- name: SearchCustomers :many
SELECT * FROM "Customer" 
WHERE book_id = sqlc.arg(book_id)
AND name ILIKE '%' || COALESCE(sqlc.narg(name), name) || '%' 
AND corporation ILIKE '%' || COALESCE(sqlc.narg(corporation), corporation) || '%'
AND address ILIKE '%' || COALESCE(sqlc.narg(address), address) || '%'
AND memo ILIKE '%' || COALESCE(sqlc.narg(memo), memo) || '%'
ORDER BY created_at DESC;

-- name: UpdateCustomer :one
UPDATE "Customer" 
SET 
  name = COALESCE(sqlc.narg(name), name),
  corporation = COALESCE(sqlc.narg(corporation), corporation),
  address = COALESCE(sqlc.narg(address), address),
  memo = COALESCE(sqlc.narg(memo), memo)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpdateCustomerCategory :one
UPDATE "Customer" 
SET category_id = sqlc.arg(category_id)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteCustomer :exec
DELETE FROM "Customer" WHERE id = $1;

-- name: GetCustomersByCategory :many
SELECT * FROM "Customer" 
WHERE book_id = $1 AND category_id = $2 
ORDER BY created_at DESC;
