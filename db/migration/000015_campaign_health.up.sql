-- Phase 27f: メール健全性チェック。
--
-- バウンス率が上がると送信ドメインの評価が落ち、以後の全メールがスパム判定
-- される。これを 3 層で防ぐ:
--
--   1. 送信前  — 宛先ドメインの MX を検証し、配信不能確定のアドレスは送らない
--                (バウンスを作らない)。結果は DomainHealth にキャッシュする。
--   2. 送信中  — バウンス率がしきい値を超えたらキャンペーンを自動一時停止
--                (サーキットブレーカー)。人が気付く前に出血を止める。
--   3. 送信元  — mailbox ドメインの SPF/DMARC/MX を点検 (DNS 実引きなので
--                テーブルは持たず RPC でオンデマンド)。

ALTER TABLE "Campaign"
  -- ハードバウンス率 (%) がこれを超えたら自動一時停止。0 で無効。
  ADD COLUMN "bounce_pause_threshold" int NOT NULL DEFAULT 5,
  -- 自動停止の理由 (空 = 自動停止されていない)。UI に出す。
  ADD COLUMN "health_paused_reason" text NOT NULL DEFAULT '';

-- 宛先ドメインの MX 検証キャッシュ。DNS を受信者ごとに引かないための表。
-- 会社スコープを持たない (DNS は全社共通の事実)。
CREATE TABLE "DomainHealth" (
  "domain"     varchar PRIMARY KEY,          -- 小文字化済み
  "has_mx"     boolean NOT NULL,             -- false = 配信不能確定
  "mx_host"    varchar NOT NULL DEFAULT '',  -- 代表 MX ホスト (デバッグ用)
  "checked_at" timestamptz NOT NULL DEFAULT (now())
);
