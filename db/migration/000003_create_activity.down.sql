-- Down: Activity テーブルを削除 + system user の seed を消す
-- 注意: Activity にしか無いデータ (email 行等) はこの操作で失われる
DELETE FROM "User" WHERE id = 'system';
DROP TABLE IF EXISTS "Activity";
