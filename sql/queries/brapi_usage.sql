-- name: RecordBrapiRequests :one
INSERT INTO brapi_usage (day, requests)
VALUES ($1, $2)
ON CONFLICT (day) DO UPDATE SET requests = brapi_usage.requests + EXCLUDED.requests
RETURNING *;

-- name: ListBrapiUsage :many
SELECT * FROM brapi_usage
WHERE day BETWEEN $1 AND $2
ORDER BY day;

-- name: SumBrapiUsage :one
SELECT COALESCE(sum(requests), 0)::bigint AS requests
FROM brapi_usage
WHERE day BETWEEN $1 AND $2;
