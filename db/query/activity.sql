-- name: CreateActivity :one
-- duration_seconds / recording_url / zoom_call_id は call type で Zoom 連携時に
-- セットされる。他の type (email_sent / email_received / manual call) では
-- 全て NULL を渡せば良い (列は nullable)。
INSERT INTO "Activity" (
    id, customer_id, contact_id, type, user_id, status_id,
    phone, mail_from, mail_to, mail_cc, subject, body, message_id,
    occurred_at,
    duration_seconds, recording_url, zoom_call_id
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12, $13,
    $14,
    $15, $16, $17
)
RETURNING *;

-- name: GetActivity :one
SELECT * FROM "Activity" WHERE id = $1;

-- name: GetActivityByMessageID :one
SELECT * FROM "Activity" WHERE message_id = $1;

-- name: GetActivityByZoomCallID :one
-- recording_completed webhook で受け取った call_id から既存 Activity を逆引き。
-- partial unique index `activity_zoom_call_id_uniq` で 1 行に絞られる。
SELECT * FROM "Activity" WHERE zoom_call_id = $1;

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
    a.duration_seconds,
    a.recording_url,
    a.zoom_call_id,
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

-- name: UpdateActivityRecordingURL :exec
-- phone.recording_completed イベントで recording archive 完了後に呼ぶ。
-- $1 = zoom_call_id (= 既存 Activity row の dedup キー)
-- $2 = recording の新パス (s3://phox-recordings/calls/.../recording.m4a)
UPDATE "Activity"
SET recording_url = $2,
    updated_at    = now()
WHERE zoom_call_id = $1;

-- name: GetMostRecentActivityForCustomer :one
-- Zoom Phone webhook の caller/callee マッチで複数 Customer 候補が出た時に
-- 「最も最近 Activity がある Customer」を選ぶための補助 query。
-- @before = 通話発生時刻、それより前の最新 Activity を返す。
SELECT id, customer_id, type, occurred_at
FROM "Activity"
WHERE customer_id = $1
  AND occurred_at < @before
ORDER BY occurred_at DESC
LIMIT 1;

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

-- name: FindCustomersByPhoneDigits :many
-- 電話番号下 10 桁マッチで Customer / Contact を引く。
-- $1 = 比較対象の数字のみ末尾 10 桁 (E.164 / 0X-XX 形式どちらでもアプリ側で正規化)。
-- 日本の番号は携帯 11 桁 + 国コード省略時 10 桁という構造で、末尾 10 桁が一致
-- すれば同一番号と判定できる (規約上、市外局番含む末尾 10 桁が unique)。
-- 1 つの番号が複数 Customer/Contact に紐づく場合 (= 家族で共有 etc) は
-- 全行返し、呼び出し側が occurred_at ベースで disambiguation する。
SELECT
    'customer'::text   AS source,
    c.id               AS customer_id,
    NULL::uuid         AS contact_id,
    c.name             AS name,
    c.book_id          AS book_id
FROM "Customer" c
WHERE c.phone IS NOT NULL
  AND c.phone <> ''
  AND length(regexp_replace(c.phone, '[^0-9]', '', 'g')) >= 10
  AND right(regexp_replace(c.phone, '[^0-9]', '', 'g'), 10) = $1::text
UNION ALL
SELECT
    'contact'::text    AS source,
    ct.customer_id     AS customer_id,
    ct.id              AS contact_id,
    ct.name            AS name,
    c.book_id          AS book_id
FROM "Contact" ct
JOIN "Customer" c ON c.id = ct.customer_id
WHERE ct.phone IS NOT NULL
  AND ct.phone <> ''
  AND length(regexp_replace(ct.phone, '[^0-9]', '', 'g')) >= 10
  AND right(regexp_replace(ct.phone, '[^0-9]', '', 'g'), 10) = $1::text;
