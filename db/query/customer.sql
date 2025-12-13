-- name: CreateCustomer :one
INSERT INTO "Customer" (
    id, book_id, phone, category, name, corporation, address, memo
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
) RETURNING *;

-- name: GetCustomer :one
SELECT
    id,
    book_id,
    phone,
    category,
    name,
    corporation,
    address,
    memo,
    updated_at,
    created_at
FROM "Customer"
WHERE id = $1;

-- name: GetCustomerByBookId :one
SELECT
    id,
    book_id,
    phone,
    category,
    name,
    corporation,
    address,
    memo,
    updated_at,
    created_at
FROM "Customer"
WHERE book_id = $1;

-- name: ListCustomers :many
SELECT
    id,
    book_id,
    phone,
    category,
    name,
    corporation,
    address,
    memo,
    updated_at,
    created_at
FROM "Customer"
WHERE book_id = $1
ORDER BY updated_at DESC
LIMIT $2
OFFSET $3;

-- name: UpdateCustomer :one
UPDATE "Customer"
SET
    phone = COALESCE(sqlc.narg(phone), phone),
    category = COALESCE(sqlc.narg(category), category),
    name = COALESCE(sqlc.narg(name), name),
    corporation = COALESCE(sqlc.narg(corporation), corporation),
    address = COALESCE(sqlc.narg(address), address),
    memo = COALESCE(sqlc.narg(memo), memo),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteCustomer :exec
DELETE FROM "Customer" WHERE id = $1;

-- name: GetCustomerCount :one
SELECT COUNT(*) FROM "Customer" WHERE book_id = $1;

-- name: GetCustomerCountByCorporation :one
SELECT COUNT(*) FROM "Customer" WHERE book_id = $1 AND corporation = $2;

-- name: GetCustomerCountByCategory :one
SELECT COUNT(*) FROM "Customer" WHERE book_id = $1 AND category = $2;

-- name: GetCustomerCountByAddress :one
SELECT COUNT(*) FROM "Customer" WHERE book_id = $1 AND address = $2;

-- name: GetCustomerCountByDate :one
SELECT COUNT(*) FROM "Customer" WHERE book_id = $1 AND created_at >= $2 AND created_at <= $3;
