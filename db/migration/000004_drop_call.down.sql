-- Phase 17 down: Call テーブルを schema-only で再作成。データは復元されない。
CREATE TABLE "Call" (
  "id" uuid PRIMARY KEY,
  "customer_id" uuid NOT NULL,
  "phone" varchar NOT NULL,
  "user_id" varchar NOT NULL,
  "status_id" uuid NOT NULL,
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

ALTER TABLE "Call" ADD FOREIGN KEY ("status_id") REFERENCES "Status" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
ALTER TABLE "Call" ADD FOREIGN KEY ("user_id") REFERENCES "User" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;
ALTER TABLE "Call" ADD FOREIGN KEY ("customer_id") REFERENCES "Customer" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;
