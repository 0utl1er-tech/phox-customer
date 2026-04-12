-- Phase 20: Google Calendar 連携のための per-user OAuth トークン保管。
-- refresh_token は AES-GCM で暗号化してから bytea に入れる (env GCAL_TOKEN_KEY)。
-- access_token はキャッシュで、expiry 切れで refresh_token から再取得される。

CREATE TABLE "UserGoogleToken" (
    "user_id"       varchar PRIMARY KEY,
    "refresh_token" bytea    NOT NULL,
    "access_token"  bytea,
    "expiry"        timestamptz,
    "scopes"        text     NOT NULL,
    "google_email"  varchar  NOT NULL DEFAULT '',
    "created_at"    timestamptz NOT NULL DEFAULT (now()),
    "updated_at"    timestamptz NOT NULL DEFAULT (now())
);

ALTER TABLE "UserGoogleToken"
  ADD FOREIGN KEY (user_id) REFERENCES "User"(id) ON DELETE CASCADE;
