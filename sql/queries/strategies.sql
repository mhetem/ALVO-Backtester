-- name: ListStrategies :many
SELECT * FROM strategies
WHERE user_id = $1
ORDER BY name;

-- name: GetStrategy :one
SELECT * FROM strategies
WHERE id = $1 AND user_id = $2;

-- name: CountStrategies :one
SELECT COUNT(*) FROM strategies
WHERE user_id = $1;

-- name: CreateStrategy :one
INSERT INTO strategies (user_id, name, description, spec)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateStrategy :one
UPDATE strategies
SET name = $3,
    description = $4,
    spec = $5,
    version = version + (spec IS DISTINCT FROM $5)::int,
    updated_at = NOW()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteStrategy :execrows
DELETE FROM strategies
WHERE id = $1 AND user_id = $2;

-- name: ShareStrategy :one
UPDATE strategies
SET share_token = $3,
    shared_at = NOW()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: UnshareStrategy :one
UPDATE strategies
SET share_token = NULL,
    shared_at = NULL
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: GetSharedStrategy :one
SELECT id, name, description, version, spec, created_at, updated_at, shared_at
FROM strategies
WHERE share_token = $1;
