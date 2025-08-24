-- name: CreateCustomer :one
INSERT INTO "Customer" (
    id, book_id, category_id, name, corporation, address, leader, pic, memo
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: GetCustomer :one
SELECT * FROM "Customer" WHERE id = $1;

-- name: ListCustomers :many
SELECT * FROM "Customer" 
WHERE book_id = $1 
ORDER BY created_at DESC;

-- name: SearchCustomers :many
SELECT * FROM "Customer" 
WHERE book_id = $1 
    AND (
        name ILIKE '%' || $2 || '%' 
        OR corporation ILIKE '%' || $2 || '%'
        OR address ILIKE '%' || $2 || '%'
    )
ORDER BY created_at DESC;

-- name: UpdateCustomer :one
UPDATE "Customer" 
SET 
    name = COALESCE($2, name),
    category_id = COALESCE($3, category_id),
    corporation = COALESCE($4, corporation),
    address = COALESCE($5, address),
    leader = COALESCE($6, leader),
    pic = COALESCE($7, pic),
    memo = COALESCE($8, memo)
WHERE id = $1 
RETURNING *;

-- name: DeleteCustomer :exec
DELETE FROM "Customer" WHERE id = $1;

-- name: GetCustomersByCategory :many
SELECT * FROM "Customer" 
WHERE book_id = $1 AND category_id = $2 
ORDER BY created_at DESC;
