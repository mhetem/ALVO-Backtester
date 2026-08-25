-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (token_hash, user_id, expires_at)
VALUES ($1, $2, $3);

-- name: GetUserFromRefreshToken :one
SELECT users.id, users.email, users.created_at, users.updated_at
FROM users
INNER JOIN refresh_tokens ON refresh_tokens.user_id = users.id
WHERE refresh_tokens.token_hash = $1
  AND refresh_tokens.revoked_at IS NULL
  AND refresh_tokens.expires_at > NOW();

-- name: RevokeRefreshToken :execrows
UPDATE refresh_tokens
SET revoked_at = NOW(), updated_at = NOW()
WHERE token_hash = $1 AND revoked_at IS NULL;
