-- name: CreateBook :exec
INSERT INTO "Book" (id, name)
VALUES ($1, $2);

-- name: UpdateBook :exec
UPDATE "Book" SET name = $2 WHERE id = $1;

-- name: DeleteBook :exec
DELETE FROM "Book" WHERE id = $1;

-- name: GetBooksByUserID :many
SELECT b.id, b.name, b.created_at, p.role
FROM "Book" b
JOIN "Permit" p ON b.id = p.book_id
WHERE p.user_id = $1
ORDER BY b.created_at DESC;

-- name: GetBookByIDAndUserID :one
SELECT b.id, b.name, b.created_at, p.role
FROM "Book" b
JOIN "Permit" p ON b.id = p.book_id
WHERE b.id = $1 AND p.user_id = $2;