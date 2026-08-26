-- name: StartIngestRun :one
INSERT INTO ingest_runs (symbol_id, timeframe, range_start, range_end, status)
VALUES ($1, $2, $3, $4, 'running')
RETURNING id;

-- name: FinishIngestRun :exec
UPDATE ingest_runs
SET status      = $2,
    http_status = $3,
    bars        = $4,
    rejected    = $5,
    error       = $6,
    finished_at = NOW(),
    duration_ms = $7
WHERE id = $1;

-- name: CountCompletedIngestRuns :one
SELECT count(*)
FROM ingest_runs
WHERE symbol_id = $1
  AND timeframe = $2
  AND status IN ('ok', 'empty')
  AND range_start <= $3
  AND range_end >= $4;

-- name: ListIngestRuns :many
SELECT * FROM ingest_runs
WHERE symbol_id = $1 AND timeframe = $2
ORDER BY started_at DESC
LIMIT $3;

-- name: LatestSyncRunAt :one
SELECT started_at FROM ingest_runs
WHERE timeframe = $1 AND status IN ('ok', 'empty')
ORDER BY started_at DESC
LIMIT 1;

-- name: ListRecentIngestFailures :many
SELECT * FROM ingest_runs
WHERE status = 'failed' AND started_at >= $1
ORDER BY started_at DESC
LIMIT $2;
