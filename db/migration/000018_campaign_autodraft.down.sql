DROP INDEX IF EXISTS "campaign_source_book_uniq";
ALTER TABLE "Campaign" DROP COLUMN IF EXISTS "source_book_id";
DROP TABLE IF EXISTS "CampaignAutoDraft";
