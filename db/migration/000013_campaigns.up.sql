-- Phase 27: コールドメール一斉送信 (キャンペーン)。
--
-- Campaign は会社スコープの「受信者スナップショット + 本文テンプレート +
-- ペーシング設定 + 送信 Mailbox プール」。受信者は作成時に確定し (snapshot)、
-- 実行中にリストが動的に変わることはない。
--
-- 紐付けの背骨は Message-ID 規約 `cmp-{recipient_uuid}@<domain>`:
-- 返信 (In-Reply-To/References)、バウンス (DSN の Original-Message-ID)、
-- Activity の dedup が全てこの ID で解決する。
--
-- 特定電子メール法対応: sender_org / sender_address / sender_contact は
-- StartCampaign で非空を検証し、全メールのフッターに自動挿入する。

CREATE TABLE "Campaign" (
  "id"           uuid PRIMARY KEY,
  "company_id"   uuid NOT NULL,
  "created_by"   varchar NOT NULL,               -- User.id (送信 Activity の user_id にも使う)
  "name"         varchar NOT NULL,
  "status"       varchar NOT NULL DEFAULT 'draft', -- draft|running|paused|completed|cancelled
  "subject"      text NOT NULL DEFAULT '',
  "body"         text NOT NULL DEFAULT '',        -- {{placeholder}} 入りテンプレート
  "track_opens"  boolean NOT NULL DEFAULT true,   -- false なら純 text/plain (27b で使用)
  "track_clicks" boolean NOT NULL DEFAULT true,
  -- ペーシング (worker は Asia/Tokyo で解釈)
  "send_start_hour"       int NOT NULL DEFAULT 9,
  "send_end_hour"         int NOT NULL DEFAULT 18,
  "send_days"             int NOT NULL DEFAULT 31, -- bitmask Mon=1..Sun=64 (default 平日)
  "daily_cap_per_mailbox" int NOT NULL DEFAULT 100,
  "min_interval_sec"      int NOT NULL DEFAULT 90, -- worker が ±50% jitter を掛ける
  "warmup_enabled"        boolean NOT NULL DEFAULT true, -- min(cap, 20 + 10*経過日) で漸増
  -- 特定電子メール法: 送信者表示 (フッターに自動挿入)
  "sender_org"     varchar NOT NULL DEFAULT '',   -- 会社名/氏名
  "sender_address" varchar NOT NULL DEFAULT '',   -- 住所
  "sender_contact" varchar NOT NULL DEFAULT '',   -- 電話 or 問い合わせ先
  "started_at"   timestamptz,
  "completed_at" timestamptz,
  "created_at"   timestamptz NOT NULL DEFAULT (now()),
  "updated_at"   timestamptz NOT NULL DEFAULT (now()),
  CONSTRAINT campaign_status_valid CHECK (status IN ('draft','running','paused','completed','cancelled'))
);

CREATE INDEX "campaign_company_created_idx" ON "Campaign" ("company_id", "created_at" DESC);

ALTER TABLE "Campaign"
  ADD FOREIGN KEY ("company_id") REFERENCES "Company"("id") ON DELETE CASCADE;
ALTER TABLE "Campaign"
  ADD FOREIGN KEY ("created_by") REFERENCES "User"("id") ON DELETE CASCADE;

-- ローテーションプール: このキャンペーンがどの Mailbox から送るか
CREATE TABLE "CampaignMailbox" (
  "campaign_id" uuid NOT NULL,
  "mailbox_id"  uuid NOT NULL,
  PRIMARY KEY ("campaign_id", "mailbox_id")
);
ALTER TABLE "CampaignMailbox"
  ADD FOREIGN KEY ("campaign_id") REFERENCES "Campaign"("id") ON DELETE CASCADE;
ALTER TABLE "CampaignMailbox"
  ADD FOREIGN KEY ("mailbox_id") REFERENCES "Mailbox"("id") ON DELETE CASCADE;

-- 受信者スナップショット + ステートマシン。
-- status は送信ライフサイクル (queued|sending|sent|failed|skipped)。
-- opened/replied/bounced/unsubscribed は overlay の時刻列で持つ (sent 後の
-- 出来事であり、状態遷移を複雑にしないため)。集計はこの非正規化列を
-- FILTER 集計するだけで済む (CampaignEvent への join 不要)。
CREATE TABLE "CampaignRecipient" (
  "id"          uuid PRIMARY KEY,                 -- Message-ID / トラッキングトークンに使う
  "campaign_id" uuid NOT NULL,
  "customer_id" uuid NOT NULL,
  "email"       varchar NOT NULL,                 -- 作成時 snapshot (小文字化済み)
  "status"      varchar NOT NULL DEFAULT 'queued',
  "attempts"    int NOT NULL DEFAULT 0,           -- transient エラーの再試行回数
  "mailbox_id"  uuid,                             -- 実際に送った Mailbox
  "message_id"  varchar,                          -- cmp-{id}@<domain> (送信時に確定)
  "error"       text NOT NULL DEFAULT '',
  "sent_at"          timestamptz,
  "first_opened_at"  timestamptz,
  "first_clicked_at" timestamptz,
  "replied_at"       timestamptz,
  "bounced_at"       timestamptz,
  "unsubscribed_at"  timestamptz,
  "locked_at"   timestamptz,                      -- stale 'sending' 検出 (janitor 用)
  "created_at"  timestamptz NOT NULL DEFAULT (now()),
  CONSTRAINT campaign_recipient_status_valid CHECK (status IN ('queued','sending','sent','failed','skipped'))
);

CREATE UNIQUE INDEX "campaign_recipient_campaign_customer_uniq" ON "CampaignRecipient" ("campaign_id", "customer_id");
-- 同一アドレス共有顧客の重複排除はスナップショット作成時にアプリ側で行う
-- (skipped 行は email が空/重複し得るため DB unique にはしない)。
-- email 索引は返信フォールバック (FindSentRecipientByFromAddr) 用。
CREATE INDEX "campaign_recipient_email_idx" ON "CampaignRecipient" ("email");
-- worker の claim 対象引き当て
CREATE INDEX "campaign_recipient_claim_idx" ON "CampaignRecipient" ("campaign_id", "status") WHERE status = 'queued';
-- 返信/DSN からの逆引き
CREATE UNIQUE INDEX "campaign_recipient_message_id_uniq" ON "CampaignRecipient" ("message_id") WHERE message_id IS NOT NULL;
-- mailbox 毎の当日送信数 (全キャンペーン横断の daily cap)
CREATE INDEX "campaign_recipient_mailbox_sent_idx" ON "CampaignRecipient" ("mailbox_id", "sent_at") WHERE sent_at IS NOT NULL;

ALTER TABLE "CampaignRecipient"
  ADD FOREIGN KEY ("campaign_id") REFERENCES "Campaign"("id") ON DELETE CASCADE;
ALTER TABLE "CampaignRecipient"
  ADD FOREIGN KEY ("customer_id") REFERENCES "Customer"("id") ON DELETE CASCADE;
ALTER TABLE "CampaignRecipient"
  ADD FOREIGN KEY ("mailbox_id") REFERENCES "Mailbox"("id") ON DELETE SET NULL;

-- クリックリダイレクト先 (27b)。URL は DB 持ち — トークンに URL を乗せない
-- ことで open redirect を作らない。
CREATE TABLE "CampaignLink" (
  "campaign_id" uuid NOT NULL,
  "idx"         int  NOT NULL,                    -- レンダリング済み本文中の URL 位置
  "url"         text NOT NULL,
  PRIMARY KEY ("campaign_id", "idx")
);
ALTER TABLE "CampaignLink"
  ADD FOREIGN KEY ("campaign_id") REFERENCES "Campaign"("id") ON DELETE CASCADE;

-- 受信者毎のイベントログ (open|click|unsubscribe|reply|bounce)。
-- ダッシュボードのドリルダウン用。集計は CampaignRecipient の時刻列を使う。
CREATE TABLE "CampaignEvent" (
  "id"           uuid PRIMARY KEY,
  "recipient_id" uuid NOT NULL,
  "kind"         varchar NOT NULL,
  "url"          text NOT NULL DEFAULT '',
  "user_agent"   varchar NOT NULL DEFAULT '',
  "created_at"   timestamptz NOT NULL DEFAULT (now()),
  CONSTRAINT campaign_event_kind_valid CHECK (kind IN ('open','click','unsubscribe','reply','bounce'))
);
CREATE INDEX "campaign_event_recipient_kind_idx" ON "CampaignEvent" ("recipient_id", "kind");
ALTER TABLE "CampaignEvent"
  ADD FOREIGN KEY ("recipient_id") REFERENCES "CampaignRecipient"("id") ON DELETE CASCADE;

-- 会社単位のサプレッションリスト (配信停止 + ハードバウンス + 手動)。
-- スナップショット作成時と送信直前の両方でチェックし、以後の全キャンペーン
-- から自動除外する。email は小文字化して格納。
CREATE TABLE "Suppression" (
  "id"          uuid PRIMARY KEY,
  "company_id"  uuid NOT NULL,
  "email"       varchar NOT NULL,
  "reason"      varchar NOT NULL,                 -- unsubscribe|hard_bounce|manual|complaint
  "campaign_id" uuid,                             -- 由来キャンペーン (あれば)
  "note"        text NOT NULL DEFAULT '',
  "created_at"  timestamptz NOT NULL DEFAULT (now()),
  CONSTRAINT suppression_reason_valid CHECK (reason IN ('unsubscribe','hard_bounce','manual','complaint'))
);
CREATE UNIQUE INDEX "suppression_company_email_uniq" ON "Suppression" ("company_id", "email");
ALTER TABLE "Suppression"
  ADD FOREIGN KEY ("company_id") REFERENCES "Company"("id") ON DELETE CASCADE;
ALTER TABLE "Suppression"
  ADD FOREIGN KEY ("campaign_id") REFERENCES "Campaign"("id") ON DELETE SET NULL;
