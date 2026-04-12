-- Phase 20: 再設計された Redial テーブルの CRUD。
-- 旧の CreateRedial (date+time 引数) は削除、start_at/end_at + note + gcal_event_id。

-- name: CreateRedial :one
INSERT INTO "Redial" (
    id, customer_id, user_id, phone, start_at, end_at, note,
    gcal_event_id, gcal_synced_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: GetRedial :one
SELECT * FROM "Redial" WHERE id = $1;

-- name: ListRedialsByCustomer :many
SELECT r.*
FROM "Redial" r
WHERE r.customer_id = $1
ORDER BY r.start_at ASC;

-- name: UpdateRedial :one
UPDATE "Redial"
SET
    phone = $2,
    start_at = $3,
    end_at = $4,
    note = $5,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetRedialGcalSynced :one
UPDATE "Redial"
SET
    gcal_event_id = $2,
    gcal_synced_at = now(),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearRedialGcalSync :one
UPDATE "Redial"
SET
    gcal_event_id = NULL,
    gcal_synced_at = NULL,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteRedial :exec
DELETE FROM "Redial" WHERE id = $1;

-- name: FindUnsyncedRedialsByUser :many
SELECT * FROM "Redial"
WHERE user_id = $1 AND gcal_event_id IS NULL
ORDER BY start_at ASC;

-- name: ListRedialsByUserWithCustomer :many
-- Phase 20e: iCal feed generation 用。
-- 1 ユーザーの (past 90 day + all future) の redial を customer name / book_id
-- 付きで返す。N+1 を避けるため JOIN で 1 クエリ。
-- 上限 1000 行 (DoS / OOM ガード)。
SELECT
    r.id, r.customer_id, r.user_id, r.phone, r.start_at, r.end_at,
    r.note, r.gcal_event_id, r.gcal_synced_at, r.updated_at, r.created_at,
    c.name AS customer_name,
    c.book_id AS customer_book_id
FROM "Redial" r
JOIN "Customer" c ON c.id = r.customer_id
WHERE r.user_id = sqlc.arg(user_id)::varchar
  AND r.start_at >= sqlc.arg(start_at_min)::timestamptz
ORDER BY r.start_at ASC
LIMIT 1000;
