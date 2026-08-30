ALTER TABLE "CampaignRecipient"
  DROP COLUMN IF EXISTS "first_message_id",
  DROP COLUMN IF EXISTS "next_step_at",
  DROP COLUMN IF EXISTS "current_step";
DROP TABLE IF EXISTS "CampaignStep";
