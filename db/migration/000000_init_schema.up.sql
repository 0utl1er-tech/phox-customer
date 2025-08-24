-- SQL dump generated using DBML (dbml.dbdiagram.io)
-- Database: PostgreSQL
-- Generated at: 2025-08-24T00:25:58.091Z

CREATE TYPE "role" AS ENUM (
  'viewer',
  'editor',
  'owner'
);

CREATE TABLE "Book" (
  "id" uuid PRIMARY KEY,
  "name" varchar NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "Permit" (
  "id" uuid PRIMARY KEY,
  "book_id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "role" role NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "Category" (
  "id" uuid PRIMARY KEY,
  "name" varchar NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "Customer" (
  "id" uuid PRIMARY KEY,
  "book_id" uuid NOT NULL,
  "category_id" uuid,
  "name" varchar NOT NULL,
  "corporation" varchar,
  "address" varchar,
  "leader" uuid UNIQUE,
  "pic" uuid UNIQUE,
  "memo" text,
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

COMMENT ON COLUMN "Customer"."leader" IS '代表者';

COMMENT ON COLUMN "Customer"."pic" IS '担当者';

ALTER TABLE "Customer" ADD FOREIGN KEY ("book_id") REFERENCES "Book" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Customer" ADD FOREIGN KEY ("category_id") REFERENCES "Category" ("id");

ALTER TABLE "Permit" ADD FOREIGN KEY ("book_id") REFERENCES "Book" ("id");
