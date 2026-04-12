-- name: GetUserICalFeed :one
SELECT * FROM "UserICalFeed" WHERE user_id = $1;

-- name: FindUserICalFeedByToken :one
SELECT * FROM "UserICalFeed" WHERE token = $1;

-- name: UpsertUserICalFeed :one
INSERT INTO "UserICalFeed" (user_id, token)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET
    token = EXCLUDED.token,
    updated_at = now()
RETURNING *;

-- name: DeleteUserICalFeed :exec
DELETE FROM "UserICalFeed" WHERE user_id = $1;
