-- name: CreateCustomer :one
INSERT INTO "Customer" (
    id, book_id, phone, category, name, corporation, address, memo, mail
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
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
    mail,
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
    mail,
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
    mail,
    updated_at,
    created_at
FROM "Customer"
WHERE book_id = $1
ORDER BY updated_at DESC
LIMIT $2
OFFSET $3;

-- name: ListAllCustomers :many
SELECT
    id,
    book_id,
    phone,
    category,
    name,
    corporation,
    address,
    memo,
    mail,
    updated_at,
    created_at
FROM "Customer"
ORDER BY created_at ASC;

-- name: UpdateCustomer :one
UPDATE "Customer"
SET
    phone = COALESCE(sqlc.narg(phone), phone),
    category = COALESCE(sqlc.narg(category), category),
    name = COALESCE(sqlc.narg(name), name),
    corporation = COALESCE(sqlc.narg(corporation), corporation),
    address = COALESCE(sqlc.narg(address), address),
    memo = COALESCE(sqlc.narg(memo), memo),
    mail = COALESCE(sqlc.narg(mail), mail),
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

-- name: FindCustomerByBookAndEmail :one
-- MCP create_customer の upsert 判定用。book 内で Customer.mail / Contact.mail の
-- どちらかが一致する最初の Customer を返す。
SELECT customer_id FROM (
    SELECT c.id AS customer_id, 1 AS priority
    FROM "Customer" c
    WHERE c.book_id = $1 AND c.mail = $2 AND c.mail <> ''
    UNION ALL
    SELECT ct.customer_id, 2 AS priority
    FROM "Contact" ct
    JOIN "Customer" c ON c.id = ct.customer_id
    WHERE c.book_id = $1 AND ct.mail = $2 AND ct.mail <> ''
) hits
ORDER BY priority
LIMIT 1;
