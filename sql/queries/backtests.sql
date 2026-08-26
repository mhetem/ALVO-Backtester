-- name: CreateBacktestRun :one
INSERT INTO backtest_runs (
    user_id, strategy_id, spec, symbol_id, timeframe,
    start_date, end_date, capital_cents, status
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, 'queued'
)
RETURNING *;

-- name: GetBacktestRun :one
SELECT r.*, s.ticker
FROM backtest_runs r
JOIN symbols s ON s.id = r.symbol_id
WHERE r.id = $1 AND r.user_id = $2;

-- name: CountActiveBacktestRuns :one
SELECT COUNT(*) FROM backtest_runs
WHERE user_id = $1 AND status IN ('queued', 'running');

-- name: ClaimBacktestRun :one
UPDATE backtest_runs
SET status = 'running',
    started_at = NOW(),
    error = NULL
WHERE id = (
    SELECT id FROM backtest_runs
    WHERE status = 'queued'
    ORDER BY created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: FinishBacktestRun :exec
UPDATE backtest_runs
SET status = 'done',
    metrics = $2,
    error = NULL,
    finished_at = NOW()
WHERE id = $1;

-- name: FailBacktestRun :exec
UPDATE backtest_runs
SET status = 'error',
    error = $2,
    finished_at = NOW()
WHERE id = $1;

-- name: RequeueBacktestRun :exec
UPDATE backtest_runs
SET status = 'queued',
    started_at = NULL
WHERE id = $1 AND status = 'running';

-- name: RequeueStaleBacktestRuns :execrows
UPDATE backtest_runs
SET status = 'queued',
    started_at = NULL
WHERE status = 'running' AND started_at < $1::timestamptz;

-- name: CreateBacktestTrades :copyfrom
INSERT INTO backtest_trades (
    run_id, seq, side, qty, entry_ts, entry_price,
    exit_ts, exit_price, pnl_cents, fees_cents, exit_reason
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11
);

-- name: CreateBacktestEquity :copyfrom
INSERT INTO backtest_equity (run_id, ts, equity_cents)
VALUES ($1, $2, $3);
