-- name: CreateSweep :one
INSERT INTO backtest_sweeps (
    user_id, strategy_id, kind, spec, axes, folds, objective,
    symbol_id, timeframe, start_date, end_date, capital_cents,
    max_positions, points
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12,
    $13, $14
)
RETURNING *;

-- name: CreateSweepRuns :copyfrom
INSERT INTO backtest_runs (
    id, user_id, strategy_id, spec, symbol_id, timeframe,
    start_date, end_date, capital_cents, max_positions, status,
    sweep_id, params, point, fold, phase
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16
);

-- name: CreateSweepSymbols :copyfrom
INSERT INTO backtest_sweep_symbols (sweep_id, ord, symbol_id)
VALUES ($1, $2, $3);

-- name: GetSweep :one
SELECT w.*, s.ticker
FROM backtest_sweeps w
JOIN symbols s ON s.id = w.symbol_id
WHERE w.id = $1 AND w.user_id = $2;

-- name: GetSweepByID :one
SELECT * FROM backtest_sweeps WHERE id = $1;

-- name: ListSweeps :many
SELECT w.*, s.ticker
FROM backtest_sweeps w
JOIN symbols s ON s.id = w.symbol_id
WHERE w.user_id = $1
ORDER BY w.created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListSweepSymbols :many
SELECT ws.ord, s.id, s.ticker
FROM backtest_sweep_symbols ws
JOIN symbols s ON s.id = ws.symbol_id
WHERE ws.sweep_id = $1
ORDER BY ws.ord;

-- name: CountActiveSweeps :one
SELECT COUNT(DISTINCT w.id) FROM backtest_sweeps w
JOIN backtest_runs r ON r.sweep_id = w.id
WHERE w.user_id = $1 AND r.status IN ('queued', 'running');

-- name: SweepProgress :one
SELECT COUNT(*) AS total,
       COUNT(*) FILTER (WHERE status = 'queued') AS queued,
       COUNT(*) FILTER (WHERE status = 'running') AS running,
       COUNT(*) FILTER (WHERE status = 'done') AS done,
       COUNT(*) FILTER (WHERE status = 'error') AS failed
FROM backtest_runs
WHERE sweep_id = $1;

-- name: ListSweepRuns :many
SELECT id, params, point, fold, phase, status, metrics, error,
       start_date, end_date, created_at, finished_at
FROM backtest_runs
WHERE sweep_id = $1
ORDER BY fold, phase, point;

-- name: ListSweepFoldRuns :many
SELECT id, params, point, spec, metrics
FROM backtest_runs
WHERE sweep_id = $1 AND fold = $2 AND phase = 'in_sample' AND status = 'done'
ORDER BY point;

-- name: ReadyWalkForwardFolds :many
SELECT r.sweep_id, r.fold
FROM backtest_runs r
JOIN backtest_sweeps w ON w.id = r.sweep_id
WHERE w.kind = 'walk_forward' AND r.phase = 'in_sample'
GROUP BY r.sweep_id, r.fold
HAVING COUNT(*) FILTER (WHERE r.status IN ('queued', 'running')) = 0
   AND COUNT(*) FILTER (WHERE r.status = 'done') > 0
   AND NOT EXISTS (
       SELECT 1 FROM backtest_runs o
       WHERE o.sweep_id = r.sweep_id AND o.fold = r.fold AND o.phase = 'out_of_sample'
   );

-- name: DeleteSweep :execrows
DELETE FROM backtest_sweeps
WHERE id = $1 AND user_id = $2;
