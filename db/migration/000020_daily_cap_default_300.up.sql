-- Phase 30: 日次送信上限 (daily_cap_per_mailbox) の既定値を 100 → 300 に引き上げる。
--
-- アプリ側 (UI ウィザード / DefaultSchedule / MCP create_campaign_draft) の初期値も
-- 同時に 300 へ変更しており、列 DEFAULT はそれらと揃えるためのもの。
-- 既存行の値は変更しない (運用中キャンペーンは個別に設定済み)。
ALTER TABLE "Campaign" ALTER COLUMN "daily_cap_per_mailbox" SET DEFAULT 300;
ALTER TABLE "CampaignAutoDraft" ALTER COLUMN "daily_cap_per_mailbox" SET DEFAULT 300;
