-- Phase 20: Redial を再設計。
-- Phase 1 (init) で Redial は存在したが、実装されてないプロトのみで UI 消費 0。
-- このため DROP+CREATE でクリーンに作り直す (既存データ 0 件前提)。
-- 変更点:
--   - id: varchar → uuid (他テーブルと統一)
--   - customer_id UNIQUE 撤廃 (1 顧客に複数の掛け直し予定を持てるように)
--   - date + time → start_at / end_at timestamptz
--   - note text を追加
--   - gcal_event_id, gcal_synced_at を追加 (Google Calendar 連携用)

DROP TABLE IF EXISTS "Redial";

CREATE TABLE "Redial" (
    "id"             uuid PRIMARY KEY,
    "customer_id"    uuid NOT NULL,
    "user_id"        varchar NOT NULL,
    "phone"          varchar NOT NULL DEFAULT '',
    "start_at"       timestamptz NOT NULL,
    "end_at"         timestamptz NOT NULL,
    "note"           text NOT NULL DEFAULT '',
    "gcal_event_id"  varchar,
    "gcal_synced_at" timestamptz,
    "updated_at"     timestamptz NOT NULL DEFAULT (now()),
    "created_at"     timestamptz NOT NULL DEFAULT (now())
);

CREATE INDEX "redial_customer_start_idx"
  ON "Redial" (customer_id, start_at DESC);
CREATE INDEX "redial_user_start_idx"
  ON "Redial" (user_id, start_at);

ALTER TABLE "Redial"
  ADD FOREIGN KEY (customer_id) REFERENCES "Customer"(id) ON DELETE CASCADE;
ALTER TABLE "Redial"
  ADD FOREIGN KEY (user_id) REFERENCES "User"(id) ON DELETE CASCADE;
