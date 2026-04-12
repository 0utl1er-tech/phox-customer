-- down: 旧 Redial schema (varchar id, date+time) を schema-only で復元。
-- データは復元されない。
DROP TABLE IF EXISTS "Redial";

CREATE TABLE "Redial" (
  "id" varchar PRIMARY KEY,
  "customer_id" uuid UNIQUE NOT NULL,
  "user_id" varchar NOT NULL,
  "phone" varchar NOT NULL,
  "date" date NOT NULL DEFAULT (now()),
  "time" time NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

ALTER TABLE "Redial" ADD FOREIGN KEY ("user_id") REFERENCES "User" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;
ALTER TABLE "Redial" ADD FOREIGN KEY ("customer_id") REFERENCES "Customer" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;
