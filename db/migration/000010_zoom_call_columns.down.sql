DROP INDEX IF EXISTS "activity_zoom_call_id_uniq";
ALTER TABLE "Activity"
    DROP COLUMN IF EXISTS "zoom_call_id",
    DROP COLUMN IF EXISTS "recording_url",
    DROP COLUMN IF EXISTS "duration_seconds";
