-- name: CreateCategory :exec
INSERT INTO "Category" (id, name)
VALUES ($1, $2);

-- name: UpdateCategory :exec
UPDATE "Category" SET name = $2 WHERE id = $1;

-- name: DeleteCategory :exec
DELETE FROM "Category" WHERE id = $1;

-- name: GetCategoryByBookAndName :one
SELECT * FROM "Category" WHERE book_id = $1 AND name = $2;

-- name: UpsertCategory :one
INSERT INTO "Category" (id, book_id, name)
VALUES ($1, $2, $3)
ON CONFLICT (book_id, name) DO UPDATE SET
    name = EXCLUDED.name
RETURNING *;