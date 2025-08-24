-- SQL dump generated using DBML (dbml.dbdiagram.io)
-- Database: PostgreSQL
-- Generated at: 2025-08-24T04:36:19.903Z

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
  "book_id" uuid NOT NULL,
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
  "memo" text,
  "created_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE UNIQUE INDEX ON "Category" ("name", "book_id");

ALTER TABLE "Customer" ADD FOREIGN KEY ("book_id") REFERENCES "Book" ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE "Customer" ADD FOREIGN KEY ("category_id") REFERENCES "Category" ("id");

ALTER TABLE "Permit" ADD FOREIGN KEY ("book_id") REFERENCES "Book" ("id");

ALTER TABLE "Category" ADD FOREIGN KEY ("book_id") REFERENCES "Book" ("id");
