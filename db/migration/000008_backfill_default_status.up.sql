-- Phase 20b: Book に default Status を backfill する。
-- これまで CreateBook/ImportBook は Status を seed していなかったため、
-- 既存 Book は全て status_count=0 の状態 (GetDefaultStatus が NotFound を返し
-- PhoneInput の saveCallHistory がコール履歴を記録できない)。
--
-- このマイグレーションでは、Status を 1 つも持たない Book 全てに
-- 「未対応」 (priority=1, effective=false, ng=false) を 1 行作成する。
--
-- gen_random_uuid() は pgcrypto に入っているが、PostgreSQL 13 以降は
-- 追加 extension 無しで使える組み込み関数。

INSERT INTO "Status" (id, book_id, priority, name, effective, ng)
SELECT gen_random_uuid(), b.id, 1, '未対応', false, false
FROM "Book" b
WHERE NOT EXISTS (
  SELECT 1 FROM "Status" s WHERE s.book_id = b.id
);
