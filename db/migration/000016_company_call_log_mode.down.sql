ALTER TABLE "Company"
  DROP CONSTRAINT IF EXISTS "Company_call_log_mode_check";

ALTER TABLE "Company"
  DROP COLUMN IF EXISTS "call_log_mode";
