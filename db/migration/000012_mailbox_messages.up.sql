-- Phase 26: メールボックス全文取込み。
-- IMAP から取得した全メッセージ (顧客に紐付かないものも含む) を保存する。
-- Activity (顧客タイムライン) とは独立した「メールボックスの生の受信箱/送信箱」。
-- 閲覧は MailboxPermit (viewer+) で制御する。
CREATE TABLE "MailboxMessage" (
  "id" uuid PRIMARY KEY,
  "mailbox_id" uuid NOT NULL,
  "folder" varchar NOT NULL,                       -- 'INBOX' / 'Sent'
  "message_id" varchar NOT NULL,                   -- RFC822 Message-ID (bracket 無し)。dedup キー
  "from_addr" varchar NOT NULL DEFAULT '',
  "to_addrs" varchar NOT NULL DEFAULT '',          -- カンマ区切り
  "cc_addrs" varchar NOT NULL DEFAULT '',
  "subject" text NOT NULL DEFAULT '',
  "body_text" text NOT NULL DEFAULT '',            -- text/plain (無ければ HTML のタグ除去)
  "attachment_names" text NOT NULL DEFAULT '',     -- 添付ファイル名のカンマ区切り (中身は保存しない)
  "customer_id" uuid,                              -- アドレスが既知顧客に解決できた場合のみ
  "occurred_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE UNIQUE INDEX ON "MailboxMessage" ("mailbox_id", "message_id");
CREATE INDEX ON "MailboxMessage" ("mailbox_id", "occurred_at" DESC);
CREATE INDEX ON "MailboxMessage" ("customer_id") WHERE "customer_id" IS NOT NULL;

ALTER TABLE "MailboxMessage" ADD FOREIGN KEY ("mailbox_id") REFERENCES "Mailbox"("id") ON DELETE CASCADE;
ALTER TABLE "MailboxMessage" ADD FOREIGN KEY ("customer_id") REFERENCES "Customer"("id") ON DELETE SET NULL;

-- 初回全履歴バックフィルの完了印。NULL = 未同期 (worker が epoch から fetch する)。
ALTER TABLE "Mailbox" ADD COLUMN "synced_at" timestamptz;
