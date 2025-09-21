-- SQL dump generated using DBML (dbml.dbdiagram.io)
-- Database: PostgreSQL
-- Generated at: 2025-09-21T22:04:00.464Z

CREATE TYPE "role" AS ENUM (
  'viewer',
  'editor',
  'owner'
);

CREATE TABLE "Book" (
  "id" uuid PRIMARY KEY,
  "name" varchar NOT NULL,
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "Company" (
  "id" uuid PRIMARY KEY,
  "name" varchar NOT NULL,
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "User" (
  "id" varchar PRIMARY KEY,
  "company_id" uuid NOT NULL,
  "name" varchar NOT NULL,
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "Redial" (
  "id" varchar PRIMARY KEY,
  "customer_id" uuid UNIQUE NOT NULL,
  "user_id" varchar NOT NULL,
  "date" date NOT NULL DEFAULT (now()),
  "time" time NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "Permit" (
  "id" uuid PRIMARY KEY,
  "book_id" uuid NOT NULL,
  "user_id" varchar NOT NULL,
  "role" role NOT NULL,
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "Customer" (
  "id" uuid PRIMARY KEY,
  "book_id" uuid NOT NULL,
  "category" varchar NOT NULL DEFAULT '',
  "name" varchar NOT NULL DEFAULT '',
  "corporation" varchar NOT NULL DEFAULT '',
  "address" varchar NOT NULL DEFAULT '',
  "memo" text NOT NULL DEFAULT '',
  "pic" uuid UNIQUE NOT NULL,
  "leader" uuid UNIQUE NOT NULL,
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "Staff" (
  "id" uuid PRIMARY KEY,
  "name" varchar NOT NULL DEFAULT '',
  "sex" varchar NOT NULL DEFAULT '',
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "Contact" (
  "id" uuid PRIMARY KEY,
  "customer_id" uuid NOT NULL,
  "staff_id" uuid NOT NULL,
  "phone" varchar NOT NULL DEFAULT '',
  "mail" varchar NOT NULL DEFAULT '',
  "fax" varchar NOT NULL DEFAULT '',
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "Call" (
  "id" uuid PRIMARY KEY,
  "customer_id" uuid NOT NULL,
  "contact_id" uuid NOT NULL,
  "user_id" varchar NOT NULL,
  "status_id" uuid NOT NULL,
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "Status" (
  "id" uuid PRIMARY KEY,
  "book_id" uuid NOT NULL,
  "priority" int NOT NULL,
  "name" varchar NOT NULL,
  "effective" bool NOT NULL,
  "ng" bool NOT NULL,
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE UNIQUE INDEX ON "Permit" ("book_id", "user_id");

CREATE UNIQUE INDEX ON "Status" ("book_id", "name");

COMMENT ON COLUMN "Status"."effective" IS '有効数としてカウントするか';

COMMENT ON COLUMN "Status"."ng" IS 'NG';

ALTER TABLE "Customer" ADD FOREIGN KEY ("book_id") REFERENCES "Book" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Permit" ADD FOREIGN KEY ("book_id") REFERENCES "Book" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Call" ADD FOREIGN KEY ("status_id") REFERENCES "Status" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION;

ALTER TABLE "Contact" ADD FOREIGN KEY ("customer_id") REFERENCES "Customer" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Staff" ADD FOREIGN KEY ("id") REFERENCES "Customer" ("pic") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Staff" ADD FOREIGN KEY ("id") REFERENCES "Customer" ("leader") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Permit" ADD FOREIGN KEY ("user_id") REFERENCES "User" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "User" ADD FOREIGN KEY ("company_id") REFERENCES "Company" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Call" ADD FOREIGN KEY ("user_id") REFERENCES "User" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Redial" ADD FOREIGN KEY ("user_id") REFERENCES "User" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Redial" ADD FOREIGN KEY ("customer_id") REFERENCES "Customer" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Call" ADD FOREIGN KEY ("contact_id") REFERENCES "Contact" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Status" ADD FOREIGN KEY ("book_id") REFERENCES "Book" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Contact" ADD FOREIGN KEY ("staff_id") REFERENCES "Staff" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Call" ADD FOREIGN KEY ("customer_id") REFERENCES "Customer" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;
