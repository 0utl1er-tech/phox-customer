-- Activity: 顧客とのやりとりを一本化したテーブル。
-- Call (通話) と Email 送信/受信を同じテーブルに入れ、画面では type で絞り込む。
-- 既存 "Call" テーブルは Phase 12 まで残し、このマイグレーションで Activity にデータ
-- を複写する。Phase 17 で別 mig として DROP する。

CREATE TABLE "Activity" (
  "id" uuid PRIMARY KEY,
  "customer_id" uuid NOT NULL,
  "contact_id" uuid,                                  -- optional: 紐づく Contact
  "type" varchar NOT NULL,                            -- 'call' | 'email_sent' | 'email_received'
  "user_id" varchar NOT NULL,                         -- CRM 操作ユーザー (Keycloak sub) or 'system' (IMAP worker)
  "status_id" uuid,                                   -- call は必須, email は任意
  "phone" varchar,                                    -- call のみ
  "mail_from" varchar,                                -- email のみ
  "mail_to" varchar,                                  -- email のみ
  "mail_cc" varchar,                                  -- email 任意
  "subject" text,                                     -- email のみ
  "body" text,                                        -- email のみ (plain text)
  "message_id" varchar,                               -- RFC822 Message-ID, dedup key
  "occurred_at" timestamptz NOT NULL DEFAULT (now()), -- 実際にアクションが起きた時刻
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  CONSTRAINT "activity_type_valid"
    CHECK (type IN ('call','email_sent','email_received')),
  CONSTRAINT "activity_call_requires_phone"
    CHECK (type <> 'call' OR phone IS NOT NULL),
  CONSTRAINT "activity_call_requires_status"
    CHECK (type <> 'call' OR status_id IS NOT NULL),
  CONSTRAINT "activity_email_requires_to"
    CHECK (type NOT IN ('email_sent','email_received') OR mail_to IS NOT NULL)
);

CREATE INDEX "activity_customer_occurred_at_idx"
  ON "Activity" (customer_id, occurred_at DESC);
CREATE INDEX "activity_type_idx" ON "Activity" (type);
CREATE UNIQUE INDEX "activity_message_id_uniq"
  ON "Activity" (message_id) WHERE message_id IS NOT NULL;

ALTER TABLE "Activity" ADD FOREIGN KEY (customer_id)
  REFERENCES "Customer"(id) ON DELETE CASCADE;
ALTER TABLE "Activity" ADD FOREIGN KEY (contact_id)
  REFERENCES "Contact"(id) ON DELETE SET NULL;
ALTER TABLE "Activity" ADD FOREIGN KEY (user_id)
  REFERENCES "User"(id) ON DELETE CASCADE;
ALTER TABLE "Activity" ADD FOREIGN KEY (status_id)
  REFERENCES "Status"(id) ON DELETE SET NULL;

-- IMAP worker が使う system ユーザーを seed (既存 0UTL1ER 社に所属)
INSERT INTO "User" (id, company_id, name, role)
VALUES ('system', '0f036454-617a-493c-ae1f-f05efcbbb330', 'system', 'owner')
ON CONFLICT (id) DO NOTHING;

-- 既存 Call を Activity に複写 (Call テーブル自体は Phase 17 で drop)
INSERT INTO "Activity" (
  id, customer_id, type, user_id, status_id, phone,
  occurred_at, created_at, updated_at
)
SELECT
  id, customer_id, 'call', user_id, status_id, phone,
  created_at, created_at, updated_at
FROM "Call"
ON CONFLICT (id) DO NOTHING;
