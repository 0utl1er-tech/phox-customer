-- Phase 25: Phox が複数の実メールボックス (mailu アカウント) を所有し、
-- Book と同じ RBAC (owner/editor/viewer) で「誰がどのメールボックスを
-- 使えるか」を制御する。なりすまし送信を廃し、返信を実口で受け取れるように
-- するための土台。
--
-- Mailbox / MailboxPermit は Book / Permit と完全同型。パスワードは平文保存
-- せず AES-GCM 暗号化 (internal/crypto、MAILBOX_SECRET_KEY) して bytea で持つ。
-- host/port/TLS は v1 では単一 mailu 前提で共有 env から取り、行に持たせない。

CREATE TABLE "Mailbox" (
  "id"            uuid PRIMARY KEY,
  "company_id"    uuid NOT NULL,
  "address"       varchar NOT NULL,              -- 送受信アドレス (例 sales@0utl1er.tech)
  "display_name"  varchar NOT NULL DEFAULT '',   -- From 表示名
  "smtp_username" varchar NOT NULL,              -- SMTP/IMAP 認証ユーザ (通常 = address)
  "password_enc"  bytea   NOT NULL,              -- AES-GCM 暗号化済みパスワード
  "active"        boolean NOT NULL DEFAULT true, -- false で送受信対象から外す
  "created_at"    timestamptz NOT NULL DEFAULT (now()),
  "updated_at"    timestamptz NOT NULL DEFAULT (now())
);

-- 同一会社内でアドレス重複を禁止
CREATE UNIQUE INDEX "mailbox_company_address_uniq" ON "Mailbox" ("company_id", "address");

ALTER TABLE "Mailbox"
  ADD FOREIGN KEY ("company_id") REFERENCES "Company"("id") ON DELETE CASCADE;

CREATE TABLE "MailboxPermit" (
  "id"         uuid PRIMARY KEY,
  "mailbox_id" uuid NOT NULL,
  "user_id"    varchar NOT NULL,
  "role"       role NOT NULL,                     -- 既存 ENUM 流用
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE UNIQUE INDEX "mailbox_permit_mailbox_user_uniq" ON "MailboxPermit" ("mailbox_id", "user_id");

ALTER TABLE "MailboxPermit"
  ADD FOREIGN KEY ("mailbox_id") REFERENCES "Mailbox"("id") ON DELETE CASCADE;
ALTER TABLE "MailboxPermit"
  ADD FOREIGN KEY ("user_id") REFERENCES "User"("id") ON DELETE CASCADE;

-- どのメールボックスで送受信した Activity かを記録 (optional; なりすまし送信や
-- 手動記録では NULL)。FK は張るが ON DELETE SET NULL で履歴は残す。
ALTER TABLE "Activity" ADD COLUMN "mailbox_id" uuid;
ALTER TABLE "Activity"
  ADD FOREIGN KEY ("mailbox_id") REFERENCES "Mailbox"("id") ON DELETE SET NULL;
