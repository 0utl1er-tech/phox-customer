-- Phase 21+ : Zoom Phone 連携で取り込む通話メタデータを Activity に格納する。
--
-- 既存設計では Activity (type='call') は phone と status のみを記録していた。
-- Zoom Phone webhook から取り込む通話には:
--   - duration_seconds: 通話秒数
--   - recording_url:    Ceph RGW (s3://phox-recordings/calls/{call_id}/recording.m4a)
--   - zoom_call_id:     Zoom 側の一意な call_id (dedup + recording 後追い更新キー)
-- が要る。3 列とも nullable: 既存 manual call エントリ + 録音無しの通話との
-- 互換性を保つため。

ALTER TABLE "Activity"
    ADD COLUMN "duration_seconds" integer,
    ADD COLUMN "recording_url"    text,
    ADD COLUMN "zoom_call_id"     varchar;

-- zoom_call_id は同一 call_id に対する webhook が重複配信される場合の
-- dedup + recording_completed イベントから Activity を逆引きする索引。
-- partial unique で NULL は無制限に許容 (= manual call は zoom_call_id NULL)。
CREATE UNIQUE INDEX "activity_zoom_call_id_uniq"
    ON "Activity" (zoom_call_id) WHERE zoom_call_id IS NOT NULL;
