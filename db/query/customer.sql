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


-- name: GetCustomerCount :one
SELECT COUNT(*) FROM "Customer" WHERE book_id = $1;

-- name: GetCustomerCountByCorporation :one
SELECT COUNT(*) FROM "Customer" WHERE corporation = $1;

-- name: GetCustomerCountByPIC :one
SELECT COUNT(*) FROM "Customer" WHERE pic = $1;

-- name: GetCustomerCountByLeader :one
SELECT COUNT(*) FROM "Customer" WHERE leader = $1;

-- name: GetCustomerCountByCategory :one
SELECT COUNT(*) FROM "Customer" WHERE category = $1;

-- name: GetCustomerCountByAddress :one
SELECT COUNT(*) FROM "Customer" WHERE address = $1;

-- name: GetCustomerCountByDate :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2;

-- name: GetCustomerCountByDateAndBook :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3;

-- name: GetCustomerCountByDateAndCorporation :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3;

-- name: GetCustomerCountByDateAndPIC :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND pic = $3;

-- name: GetCustomerCountByDateAndLeader :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND leader = $3;

-- name: GetCustomerCountByDateAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND category = $3;

-- name: GetCustomerCountByDateAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND address = $3;

-- name: GetCustomerCountByDateAndBookAndCorporation :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4;

-- name: GetCustomerCountByDateAndBookAndPIC :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND pic = $4;

-- name: GetCustomerCountByDateAndBookAndLeader :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND leader = $4;

-- name: GetCustomerCountByDateAndBookAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND category = $4;

-- name: GetCustomerCountByDateAndBookAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND address = $4;

-- name: GetCustomerCountByDateAndCorporationAndPIC :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND pic = $4;

-- name: GetCustomerCountByDateAndCorporationAndLeader :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND leader = $4;

-- name: GetCustomerCountByDateAndCorporationAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND category = $4;

-- name: GetCustomerCountByDateAndCorporationAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND address = $4;

-- name: GetCustomerCountByDateAndPICAndLeader :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND pic = $3 AND leader = $4;

-- name: GetCustomerCountByDateAndPICAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND pic = $3 AND category = $4;

-- name: GetCustomerCountByDateAndPICAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND pic = $3 AND address = $4;

-- name: GetCustomerCountByDateAndLeaderAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND leader = $3 AND category = $4;

-- name: GetCustomerCountByDateAndLeaderAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND leader = $3 AND address = $4;

-- name: GetCustomerCountByDateAndCategoryAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND category = $3 AND address = $4;

-- name: GetCustomerCountByDateAndBookAndCorporationAndPIC :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND pic = $5;

-- name: GetCustomerCountByDateAndBookAndCorporationAndLeader :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND leader = $5;

-- name: GetCustomerCountByDateAndBookAndCorporationAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND category = $5;

-- name: GetCustomerCountByDateAndBookAndCorporationAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND address = $5;

-- name: GetCustomerCountByDateAndBookAndPICAndLeader :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND pic = $4 AND leader = $5;

-- name: GetCustomerCountByDateAndBookAndPICAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND pic = $4 AND category = $5;

-- name: GetCustomerCountByDateAndBookAndPICAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND pic = $4 AND address = $5;

-- name: GetCustomerCountByDateAndBookAndLeaderAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND leader = $4 AND category = $5;

-- name: GetCustomerCountByDateAndBookAndLeaderAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND leader = $4 AND address = $5;
-- name: GetCustomerCountByDateAndBookAndCategoryAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND category = $4 AND address = $5;

-- name: GetCustomerCountByDateAndCorporationAndPICAndLeader :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND pic = $4 AND leader = $5;

-- name: GetCustomerCountByDateAndCorporationAndPICAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND pic = $4 AND category = $5;

-- name: GetCustomerCountByDateAndCorporationAndPICAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND pic = $4 AND address = $5;

-- name: GetCustomerCountByDateAndCorporationAndLeaderAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND leader = $4 AND category = $5;

-- name: GetCustomerCountByDateAndCorporationAndLeaderAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND leader = $4 AND address = $5;

-- name: GetCustomerCountByDateAndPICAndLeaderAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND pic = $3 AND leader = $4 AND category = $5;

-- name: GetCustomerCountByDateAndPICAndLeaderAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND pic = $3 AND leader = $4 AND address = $5;

-- name: GetCustomerCountByDateAndLeaderAndCategoryAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND leader = $3 AND category = $4 AND address = $5;

-- name: GetCustomerCountByDateAndBookAndCorporationAndPICAndLeader :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND pic = $5 AND leader = $6;

-- name: GetCustomerCountByDateAndBookAndCorporationAndPICAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND pic = $5 AND category = $6;

-- name: GetCustomerCountByDateAndBookAndCorporationAndPICAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND pic = $5 AND address = $6;

-- name: GetCustomerCountByDateAndBookAndCorporationAndLeaderAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND leader = $5 AND category = $6;

-- name: GetCustomerCountByDateAndBookAndCorporationAndLeaderAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND leader = $5 AND address = $6;

-- name: GetCustomerCountByDateAndBookAndPICAndLeaderAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND pic = $4 AND leader = $5 AND category = $6;

-- name: GetCustomerCountByDateAndBookAndPICAndLeaderAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND pic = $4 AND leader = $5 AND address = $6;

-- name: GetCustomerCountByDateAndBookAndLeaderAndCategoryAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND leader = $4 AND category = $5 AND address = $6;

-- name: GetCustomerCountByDateAndCorporationAndPICAndLeaderAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND pic = $4 AND leader = $5 AND category = $6;

-- name: GetCustomerCountByDateAndCorporationAndPICAndLeaderAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND pic = $4 AND leader = $5 AND address = $6;

-- name: GetCustomerCountByDateAndCorporationAndLeaderAndCategoryAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND leader = $4 AND category = $5 AND address = $6;

-- name: GetCustomerCountByDateAndPICAndLeaderAndCategoryAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND pic = $3 AND leader = $4 AND category = $5 AND address = $6;

-- name: GetCustomerCountByDateAndBookAndCorporationAndPICAndLeaderAndCategory :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND pic = $5 AND leader = $6 AND category = $7;

-- name: GetCustomerCountByDateAndBookAndCorporationAndPICAndLeaderAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND pic = $5 AND leader = $6 AND address = $7;

-- name: GetCustomerCountByDateAndBookAndCorporationAndLeaderAndCategoryAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND leader = $5 AND category = $6 AND address = $7;

-- name: GetCustomerCountByDateAndBookAndPICAndLeaderAndCategoryAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND pic = $4 AND leader = $5 AND category = $6 AND address = $7;
-- name: GetCustomerCountByDateAndCorporationAndPICAndLeaderAndCategoryAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND corporation = $3 AND pic = $4 AND leader = $5 AND category = $6 AND address = $7;

-- name: GetCustomerCountByDateAndBookAndCorporationAndPICAndLeaderAndCategoryAndAddress :one
SELECT COUNT(*) FROM "Customer" WHERE created_at >= $1 AND created_at <= $2 AND book_id = $3 AND corporation = $4 AND pic = $5 AND leader = $6 AND category = $7 AND address = $8;
