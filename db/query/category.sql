-- name: CreateCategory :exec
INSERT INTO "Category" (id, name)
VALUES ($1, $2);

-- name: UpdateCategory :exec
UPDATE "Category" SET name = $2 WHERE id = $1;

-- name: DeleteCategory :exec
DELETE FROM "Category" WHERE id = $1;