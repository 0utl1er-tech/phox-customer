-- Phase 17: Call テーブル撤去
-- Phase 9 で既存行を Activity に複写済み、Phase 12 で proto/service/UI を撤去済み。
-- この mig で物理テーブルを削除する。
-- (ロールバックは schema-only、データ復元は不可)

DROP TABLE IF EXISTS "Call";
