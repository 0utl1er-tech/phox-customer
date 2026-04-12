-- name: GetUserGoogleToken :one
SELECT * FROM "UserGoogleToken" WHERE user_id = $1;

-- name: UpsertUserGoogleToken :one
INSERT INTO "UserGoogleToken" (
    user_id, refresh_token, access_token, expiry, scopes, google_email
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (user_id) DO UPDATE SET
    refresh_token = EXCLUDED.refresh_token,
    access_token = EXCLUDED.access_token,
    expiry = EXCLUDED.expiry,
    scopes = EXCLUDED.scopes,
    google_email = EXCLUDED.google_email,
    updated_at = now()
RETURNING *;

-- name: UpdateUserGoogleTokenAccess :one
-- refresh_token は保持、access_token と expiry だけ更新 (oauth2 library が refresh した後)。
UPDATE "UserGoogleToken"
SET
    access_token = $2,
    expiry = $3,
    updated_at = now()
WHERE user_id = $1
RETURNING *;

-- name: DeleteUserGoogleToken :exec
DELETE FROM "UserGoogleToken" WHERE user_id = $1;
