-- name: CreateMailboxMessage :one
INSERT INTO "MailboxMessage" (
  id, mailbox_id, folder, message_id, from_addr, to_addrs, cc_addrs,
  subject, body_text, attachment_names, customer_id, occurred_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: GetMailboxMessage :one
SELECT * FROM "MailboxMessage" WHERE id = $1;

-- name: ListMailboxMessages :many
-- 本文は返さない (一覧用メタデータ)。has_attachments 判定用に attachment_names は返す。
SELECT id, mailbox_id, folder, message_id, from_addr, to_addrs, cc_addrs,
       subject, attachment_names, customer_id, occurred_at, created_at
FROM "MailboxMessage"
WHERE mailbox_id = $1
  AND (sqlc.narg('folder')::varchar IS NULL OR folder = sqlc.narg('folder'))
ORDER BY occurred_at DESC
LIMIT $2 OFFSET $3;

-- name: CountMailboxMessages :one
SELECT count(*) FROM "MailboxMessage"
WHERE mailbox_id = $1
  AND (sqlc.narg('folder')::varchar IS NULL OR folder = sqlc.narg('folder'));

-- name: SetMailboxSyncedAt :exec
UPDATE "Mailbox" SET synced_at = now(), updated_at = now() WHERE id = $1;

-- name: BackfillActivitiesForCustomerEmail :execrows
-- 顧客の mail に一致する未紐付け MailboxMessage を Activity 化し、
-- MailboxMessage.customer_id も紐付ける。create_customer 後に呼ぶ。
-- INBOX(from 一致)=email_received / Sent(to 内に一致)=email_sent。
-- message_id 重複 (既に Activity 化済み) は ON CONFLICT でスキップ。冪等。
WITH matched AS (
  SELECT mm.id, mm.folder, mm.from_addr, mm.to_addrs, mm.cc_addrs,
         mm.subject, mm.body_text, mm.message_id, mm.occurred_at, mm.mailbox_id
  FROM "MailboxMessage" mm
  WHERE mm.customer_id IS NULL
    AND (
      (mm.folder = 'INBOX' AND lower(mm.from_addr) = lower(@email))
      OR (mm.folder = 'Sent' AND position(lower(@email) IN lower(mm.to_addrs)) > 0)
    )
),
ins AS (
  INSERT INTO "Activity"
    (id, customer_id, contact_id, type, user_id, mail_from, mail_to, mail_cc,
     subject, body, message_id, occurred_at, mailbox_id)
  SELECT gen_random_uuid(), @customer_id, sqlc.narg('contact_id'),
         CASE WHEN m.folder = 'Sent' THEN 'email_sent' ELSE 'email_received' END,
         'system', m.from_addr, m.to_addrs, m.cc_addrs,
         m.subject, m.body_text, m.message_id, m.occurred_at, m.mailbox_id
  FROM matched m
  ON CONFLICT (message_id) WHERE message_id IS NOT NULL DO NOTHING
)
UPDATE "MailboxMessage" mm SET customer_id = @customer_id
FROM matched m WHERE mm.id = m.id;
