ALTER TABLE "Company"
  DROP COLUMN IF EXISTS "notify_webhook_url",
  DROP COLUMN IF EXISTS "notify_events";
