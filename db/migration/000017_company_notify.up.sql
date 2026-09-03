-- Phase 27h: キャンペーン反響通知 (Company 単位の管理者設定)。
--   notify_webhook_url … Discord Webhook URL。空 = 通知無効。
--   notify_events      … 通知するイベントのカンマ区切りリスト。
--                        既知値: reply,click,unsubscribe,bounce,open
--                        デフォルトは reply のみ ON。
ALTER TABLE "Company"
  ADD COLUMN "notify_webhook_url" varchar NOT NULL DEFAULT '',
  ADD COLUMN "notify_events" varchar NOT NULL DEFAULT 'reply';
