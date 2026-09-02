DROP TABLE IF EXISTS "DomainHealth";

ALTER TABLE "Campaign"
  DROP COLUMN IF EXISTS "bounce_pause_threshold",
  DROP COLUMN IF EXISTS "health_paused_reason";
