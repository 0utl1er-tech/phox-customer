-- Phase 20e: iCalendar (.ics) 購読 URL 機能。
-- 各 CRM ユーザーが固有の URL-safe base64 トークンを 1 つ持ち、
-- http://.../ical/{token} でその URL が "text/calendar" を返す。
-- トークンは平文保存 (URL 自体が credential という設計)。

CREATE TABLE "UserICalFeed" (
    "user_id"    varchar PRIMARY KEY,
    "token"      varchar UNIQUE NOT NULL,
    "created_at" timestamptz NOT NULL DEFAULT (now()),
    "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE INDEX "user_ical_feed_token_idx" ON "UserICalFeed" (token);

ALTER TABLE "UserICalFeed"
  ADD FOREIGN KEY (user_id) REFERENCES "User"(id) ON DELETE CASCADE;
