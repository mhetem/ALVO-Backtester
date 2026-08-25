-- name: ListChartLayouts :many
SELECT * FROM chart_layouts
WHERE user_id = $1
ORDER BY name;

-- name: CountChartLayouts :one
SELECT COUNT(*) FROM chart_layouts
WHERE user_id = $1;

-- name: CreateChartLayout :one
INSERT INTO chart_layouts (user_id, name, layout)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateChartLayout :one
UPDATE chart_layouts
SET name = $3, layout = $4, updated_at = NOW()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteChartLayout :execrows
DELETE FROM chart_layouts
WHERE id = $1 AND user_id = $2;
