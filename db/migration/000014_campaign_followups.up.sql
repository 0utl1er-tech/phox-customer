-- Phase 27e: フォローアップシーケンス (Instantly のスケジュール機能)。
--
-- 1 通目はこれまで通り Campaign.subject/body。CampaignStep には
-- 2 通目以降 (step_no >= 2) のフォローアップだけを持つ — 既存キャンペーンは
-- 行が無いだけでそのまま動く (後方互換、バックフィル不要)。
--
-- 受信者側は current_step / next_step_at で進行を管理する:
--   step k 送信 → (次ステップがあれば) status='queued' に戻し
--   next_step_at = 送信時刻 + wait_days。返信/配信停止/バウンスが付いたら
--   worker がシーケンスを終了 (status='sent' で確定) する。

CREATE TABLE "CampaignStep" (
  "campaign_id" uuid NOT NULL,
  "step_no"     int  NOT NULL,                  -- 2..N (1 通目は Campaign 本体)
  "wait_days"   int  NOT NULL DEFAULT 3,        -- 前ステップ送信から何日待つか
  "subject"     text NOT NULL DEFAULT '',       -- 空 = "Re: <1通目件名>" (同一スレッド返信)
  "body"        text NOT NULL,
  PRIMARY KEY ("campaign_id", "step_no")
);
ALTER TABLE "CampaignStep"
  ADD FOREIGN KEY ("campaign_id") REFERENCES "Campaign"("id") ON DELETE CASCADE;

ALTER TABLE "CampaignRecipient"
  ADD COLUMN "current_step" int NOT NULL DEFAULT 1,
  ADD COLUMN "next_step_at" timestamptz,        -- 次ステップの送信予定 (NULL = 予定なし)
  -- スレッド維持用: 1 通目の Message-ID (References ヘッダの起点)
  ADD COLUMN "first_message_id" varchar;
