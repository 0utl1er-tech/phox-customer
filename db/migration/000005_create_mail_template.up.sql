-- Phase 19: メールテンプレート — Book に紐づく件名/本文テンプレ。
-- プレースホルダ (`{{customer_name}}` など) はフロント側で置換する。
-- backend はテンプレ文字列をそのまま保持するだけ。

CREATE TABLE "MailTemplate" (
    "id"         uuid PRIMARY KEY,
    "book_id"    uuid NOT NULL,
    "name"       varchar NOT NULL,
    "subject"    varchar NOT NULL DEFAULT '',
    "body"       text    NOT NULL DEFAULT '',
    "created_at" timestamptz NOT NULL DEFAULT (now()),
    "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE INDEX "mail_template_book_created_idx"
  ON "MailTemplate" (book_id, created_at DESC);

ALTER TABLE "MailTemplate"
  ADD FOREIGN KEY (book_id) REFERENCES "Book"(id) ON DELETE CASCADE;
