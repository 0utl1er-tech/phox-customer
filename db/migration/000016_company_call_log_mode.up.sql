-- Phase 27f: 通話記録モード (Company 単位の管理者設定)。
--   'click' … 従来どおり。電話番号クリック時にフォールバック経路でも
--             コール活動を自動記録する (デフォルト = 挙動不変)。
--   'zoom'  … Zoom 通話履歴をマスターにする。フォールバックでの自動記録を
--             廃止し、Zoom call_logs の毎時リコンシリエーションで
--             webhook 取りこぼしを回収する。
ALTER TABLE "Company"
  ADD COLUMN "call_log_mode" varchar NOT NULL DEFAULT 'click';

ALTER TABLE "Company"
  ADD CONSTRAINT "Company_call_log_mode_check"
  CHECK ("call_log_mode" IN ('click', 'zoom'));
