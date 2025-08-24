-- name: CreateBook :exec
INSERT INTO "Book" (id, name)
VALUES ($1, $2);

-- name: UpdateBook :exec
UPDATE "Book" SET name = $2 WHERE id = $1;

-- name: DeleteBook :exec
DELETE FROM "Book" WHERE id = $1;