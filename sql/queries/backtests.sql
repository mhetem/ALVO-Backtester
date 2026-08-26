-- name: CreateBacktestRun :one
INSERT INTO backtest_runs (
    user_id, strategy_id, spec, symbol_id, timeframe,
    start_date, end_date, capital_cents, max_positions, status,
    sweep_id, params, point, fold, phase
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, 'queued',
    $10, $11, $12, $13, $14
)
RETURNING *;

-- name: CreateBacktestRunSymbols :copyfrom
INSERT INTO backtest_run_symbols (run_id, ord, symbol_id)
VALUES ($1, $2, $3);

-- name: GetBacktestRun :one
SELECT r.*, s.ticker
FROM backtest_runs r
JOIN symbols s ON s.id = r.symbol_id
WHERE r.id = $1 AND r.user_id = $2;

-- name: ListBacktestRunSymbols :many
SELECT rs.ord, s.id, s.ticker, s.lot_size, s.tick_size
FROM backtest_run_symbols rs
JOIN symbols s ON s.id = rs.symbol_id
WHERE rs.run_id = $1
ORDER BY rs.ord;

-- name: ListBacktestRunTickers :many
SELECT rs.run_id, rs.ord, s.ticker
FROM backtest_run_symbols rs
JOIN symbols s ON s.id = rs.symbol_id
WHERE rs.run_id = ANY($1::uuid[])
ORDER BY rs.run_id, rs.ord;

-- name: CountActiveBacktestRuns :one
SELECT COUNT(*) FROM backtest_runs
WHERE user_id = $1 AND sweep_id IS NULL AND status IN ('queued', 'running');

-- name: ClaimBacktestRun :one
UPDATE backtest_runs
SET status = 'running',
    started_at = NOW(),
    error = NULL
WHERE id = (
    SELECT id FROM backtest_runs
    WHERE status = 'queued'
    ORDER BY (sweep_id IS NOT NULL), created_at
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
    run_id, seq, symbol_id, side, qty, entry_ts, entry_price,
    exit_ts, exit_price, pnl_cents, fees_cents, dividends_cents,
    borrow_cents, split_cash_cents, exit_reason
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12,
    $13, $14, $15
);

-- name: CreateBacktestEquity :copyfrom
INSERT INTO backtest_equity (run_id, ts, equity_cents, hold_cents, index_cents)
VALUES ($1, $2, $3, $4, $5);

-- name: ListBacktestRuns :many
SELECT r.*, s.ticker
FROM backtest_runs r
JOIN symbols s ON s.id = r.symbol_id
WHERE r.user_id = $1 AND r.sweep_id IS NULL
ORDER BY r.created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListBacktestTrades :many
SELECT t.seq, s.ticker, t.side, t.qty, t.entry_ts, t.entry_price, t.exit_ts, t.exit_price,
       t.pnl_cents, t.fees_cents, t.dividends_cents, t.borrow_cents, t.split_cash_cents,
       t.exit_reason
FROM backtest_trades t
JOIN symbols s ON s.id = t.symbol_id
WHERE t.run_id = $1
ORDER BY t.seq;

-- name: CountBacktestEquity :one
SELECT COUNT(*) FROM backtest_equity WHERE run_id = $1;

-- name: ListBacktestEquity :many
SELECT ts, equity_cents, hold_cents, index_cents
FROM (
    SELECT ts, equity_cents, hold_cents, index_cents,
           ROW_NUMBER() OVER (ORDER BY ts) AS n,
           COUNT(*) OVER () AS total
    FROM backtest_equity
    WHERE run_id = $1
) points
WHERE (n - 1) % GREATEST((total + $2::bigint - 1) / $2::bigint, 1) = 0 OR n = total
ORDER BY ts;
