-- Phase 29b: 顧客ごとの任意差し込み変数 (custom_fields)。
--
-- 目的: コールドメールで「1 件ごとに異なる診断結果」を差し込めるようにする。
-- 例: MEO 診断営業 — 店舗ごとに算出したスコアと未対応項目の一覧を本文に入れる。
--   {"meo_score": "30/100", "meo_issues": "・写真が3枚以下\n・投稿が90日以上停止"}
-- キャンペーン本文では {{fields.meo_score}} / {{fields.meo_issues}} で参照する。
--
-- 形: キー=文字列 (英小文字/数字/アンダースコア, 最大 64 文字) →
--     値=文字列 (最大 4096 バイト) のフラットな JSON オブジェクト。
--     ネストや配列は許容しない (差し込みは文字列置換なので意味がない)。
--     上限はアプリ側 (internal/customfields) で担保する。
--
-- インデックス: 付けない。custom_fields は「顧客を引いた後に読む」列で、
-- 値での検索・絞り込み要件が現時点で無い (検索は Elasticsearch 側の責務)。
-- GIN は書き込みコストと肥大を伴うため、検索要件が出てから足す。
ALTER TABLE "Customer"
  ADD COLUMN "custom_fields" jsonb NOT NULL DEFAULT '{}'::jsonb;
