-- name: UpsertCandles :batchexec
INSERT INTO candles (
    symbol_id, timeframe, ts, open, high, low, close, adj_close, volume
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (symbol_id, timeframe, ts) DO UPDATE SET
    open      = EXCLUDED.open,
    high      = EXCLUDED.high,
    low       = EXCLUDED.low,
    close     = EXCLUDED.close,
    adj_close = COALESCE(EXCLUDED.adj_close, candles.adj_close),
    volume    = EXCLUDED.volume;

-- name: ListCandles :many
SELECT ts, open, high, low, close, adj_close, volume
FROM candles
WHERE symbol_id = $1 AND timeframe = $2 AND ts >= $3 AND ts < $4
ORDER BY ts
LIMIT $5;

-- name: ListCandlesDesc :many
SELECT ts, open, high, low, close, adj_close, volume
FROM candles
WHERE symbol_id = $1 AND timeframe = $2 AND ts >= $3 AND ts < $4
ORDER BY ts DESC
LIMIT $5;

-- name: ListCandleTimestamps :many
SELECT ts
FROM candles
WHERE symbol_id = $1 AND timeframe = $2 AND ts >= $3 AND ts < $4
ORDER BY ts;

-- name: CountCandles :one
SELECT count(*)
FROM candles
WHERE symbol_id = $1 AND timeframe = $2;

-- name: CountCandlesInRange :one
SELECT count(*)
FROM candles
WHERE symbol_id = $1 AND timeframe = $2 AND ts >= $3 AND ts < $4;

-- name: EarliestCandle :one
SELECT ts, open, high, low, close, adj_close, volume
FROM candles
WHERE symbol_id = $1 AND timeframe = $2
ORDER BY ts
LIMIT 1;

-- name: LatestCandle :one
SELECT ts, open, high, low, close, adj_close, volume
FROM candles
WHERE symbol_id = $1 AND timeframe = $2
ORDER BY ts DESC
LIMIT 1;
