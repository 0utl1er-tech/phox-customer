-- name: CreateActivity :one
INSERT INTO "Activity" (
    id, customer_id, contact_id, type, user_id, status_id,
    phone, mail_from, mail_to, mail_cc, subject, body, message_id,
    occurred_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12, $13,
    $14
)
RETURNING *;

-- name: GetActivity :one
SELECT * FROM "Activity" WHERE id = $1;

-- name: GetActivityByMessageID :one
SELECT * FROM "Activity" WHERE message_id = $1;

-- name: ListActivitiesByCustomerID :many
-- types が空配列 or NULL のときは全件、非空のときは type IN (types) で絞り込む。
SELECT
    a.id,
    a.customer_id,
    a.contact_id,
    a.type,
    a.user_id,
    u.name AS user_name,
    a.status_id,
    s.name AS status_name,
    s.priority AS status_priority,
    s.effective AS status_effective,
    s.ng AS status_ng,
    a.phone,
    a.mail_from,
    a.mail_to,
    a.mail_cc,
    a.subject,
    a.body,
    a.message_id,
    a.occurred_at,
    a.created_at,
    a.updated_at
FROM "Activity" a
JOIN "User" u ON u.id = a.user_id
LEFT JOIN "Status" s ON s.id = a.status_id
WHERE a.customer_id = $1
  AND (cardinality(@types::text[]) = 0 OR a.type = ANY(@types::text[]))
ORDER BY a.occurred_at DESC;

-- name: UpdateActivityStatus :one
UPDATE "Activity"
SET
    status_id = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteActivity :exec
DELETE FROM "Activity" WHERE id = $1;

-- name: FindCustomerByEmail :one
-- IMAP worker が To アドレスから Customer を解決するのに使う。
-- Customer.mail と Contact.mail の両方を UNION で引き、最初にヒットしたものを返す。
SELECT
    customer_id,
    contact_id,
    book_id
FROM (
    SELECT c.id AS customer_id, NULL::uuid AS contact_id, c.book_id AS book_id, 1 AS priority
    FROM "Customer" c
    WHERE c.mail = $1 AND c.mail <> ''
    UNION ALL
    SELECT ct.customer_id, ct.id AS contact_id, c.book_id AS book_id, 2 AS priority
    FROM "Contact" ct
    JOIN "Customer" c ON c.id = ct.customer_id
    WHERE ct.mail = $1 AND ct.mail <> ''
) hits
ORDER BY priority
LIMIT 1;
