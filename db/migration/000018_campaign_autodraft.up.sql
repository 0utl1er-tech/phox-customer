-- Phase 28f: キャンペーン自動下書き (Book 投函 → draft 自動生成)。
--
-- 28d の lakehouse パイプラインが Google Maps 由来のリードを
-- Book (`GM_{業種}_{都道府県}_{YYYY-MM}_HPあり` / `_HPなし`) に自動投函する。
-- 本テーブルはその Book に対して「どんな営業文面でキャンペーン下書きを
-- 自動生成するか」を Book 名パターン毎に定義する設定行。
--
-- 複数行を持てるのが要件の芯: HP を持つ店 (乗り換え提案) と持たない店
-- (HP 制作の新規提案) では営業内容が全く違うため、1 テンプレでは足りない。
--
-- 自動化は下書き作成まで。status は draft のままで、送信開始 (StartCampaign)
-- は常に人間が押す。

CREATE TABLE "CampaignAutoDraft" (
  "id"                uuid PRIMARY KEY,
  "company_id"        uuid NOT NULL,
  "name"              varchar NOT NULL,               -- 管理用ラベル (例「HPあり: 乗り換え提案」)
  "enabled"           boolean NOT NULL DEFAULT false, -- 既定 OFF — 明示的に有効化するまで何も起きない
  -- SQL LIKE パターン (例 `GM\_%\_HPあり`)。ESCAPE 既定の \ でリテラル _ を守る。
  "book_name_pattern" varchar NOT NULL,
  -- 生成するキャンペーンの中身 (Campaign と同型)
  "subject"           text NOT NULL DEFAULT '',
  "body"              text NOT NULL DEFAULT '',
  -- フォローアップ: [{"wait_days":3,"subject":"","body":"..."}] (CampaignStep と同型)
  "followups"         jsonb NOT NULL DEFAULT '[]'::jsonb,
  "mailbox_ids"       uuid[] NOT NULL DEFAULT '{}',
  "track_opens"       boolean NOT NULL DEFAULT true,
  "track_clicks"      boolean NOT NULL DEFAULT true,
  -- 送信ペーシングの既定値 (Campaign の schedule 列と同型)
  "send_start_hour"       int NOT NULL DEFAULT 9,
  "send_end_hour"         int NOT NULL DEFAULT 18,
  "send_days"             int NOT NULL DEFAULT 31,
  "daily_cap_per_mailbox" int NOT NULL DEFAULT 100,
  "min_interval_sec"      int NOT NULL DEFAULT 90,
  "warmup_enabled"        boolean NOT NULL DEFAULT true,
  "bounce_pause_threshold" int NOT NULL DEFAULT 5,
  -- 特定電子メール法の送信者表示 (フッターに自動挿入)
  "sender_org"        varchar NOT NULL DEFAULT '',
  "sender_address"    varchar NOT NULL DEFAULT '',
  "sender_contact"    varchar NOT NULL DEFAULT '',
  -- 下書きの名義。この User の Book/Mailbox 権限で作成する
  -- (権限が無い Book はスキップして warning ログのみ)。
  "creator_user_id"   varchar NOT NULL,
  -- 最後に下書きを自動生成した時刻 (UI 表示用。生成が無ければ NULL)。
  "last_created_at"   timestamptz,
  "created_at"        timestamptz NOT NULL DEFAULT (now()),
  "updated_at"        timestamptz NOT NULL DEFAULT (now())
);

CREATE INDEX "campaign_autodraft_company_idx" ON "CampaignAutoDraft" ("company_id", "created_at");
-- worker の tick 対象引き当て (有効なテンプレのみ)。
CREATE INDEX "campaign_autodraft_enabled_idx" ON "CampaignAutoDraft" ("enabled") WHERE enabled;

ALTER TABLE "CampaignAutoDraft"
  ADD FOREIGN KEY ("company_id") REFERENCES "Company"("id") ON DELETE CASCADE;
ALTER TABLE "CampaignAutoDraft"
  ADD FOREIGN KEY ("creator_user_id") REFERENCES "User"("id") ON DELETE CASCADE;

-- 「この Book には既に自動下書きを作った」の判定軸。Book 名でキャンペーン名を
-- 決定的にして照合するより確実 (キャンペーン名は人が編集できるため)。
-- 手動作成のキャンペーンは NULL のまま = 自動生成の判定に影響しない。
ALTER TABLE "Campaign" ADD COLUMN "source_book_id" uuid;
ALTER TABLE "Campaign"
  ADD FOREIGN KEY ("source_book_id") REFERENCES "Book"("id") ON DELETE SET NULL;

-- at-most-once の担保: 同じ Book から自動下書きは 1 本まで。replicas=2 の
-- 両 pod が同時に tick しても、後発の INSERT は 23505 で落ちて no-op になる。
CREATE UNIQUE INDEX "campaign_source_book_uniq"
  ON "Campaign" ("source_book_id") WHERE source_book_id IS NOT NULL;
