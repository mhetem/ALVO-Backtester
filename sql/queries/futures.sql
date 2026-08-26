-- name: UpsertFuturesContract :one
INSERT INTO futures_contracts (
    symbol, root, description, segment, multiplier, lot_size, currency, isin, first_trade, last_trade, expiration
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
ON CONFLICT (symbol) DO UPDATE SET
    root        = EXCLUDED.root,
    description = EXCLUDED.description,
    segment     = EXCLUDED.segment,
    multiplier  = EXCLUDED.multiplier,
    lot_size    = EXCLUDED.lot_size,
    currency    = EXCLUDED.currency,
    isin        = EXCLUDED.isin,
    first_trade = EXCLUDED.first_trade,
    last_trade  = EXCLUDED.last_trade,
    expiration  = EXCLUDED.expiration,
    seen_at     = NOW()
RETURNING *;

-- name: ListFuturesContractsByRoot :many
SELECT * FROM futures_contracts
WHERE root = $1
ORDER BY expiration;

-- name: ListFuturesRoots :many
SELECT DISTINCT root FROM futures_contracts
ORDER BY root;

-- name: GetFuturesContract :one
SELECT * FROM futures_contracts
WHERE symbol = $1;

-- name: UpsertFuturesQuotes :batchexec
INSERT INTO futures_quotes (
    contract_id, day, settlement, high, low, close, average, volume, trades
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (contract_id, day) DO UPDATE SET
    settlement = EXCLUDED.settlement,
    high       = COALESCE(EXCLUDED.high, futures_quotes.high),
    low        = COALESCE(EXCLUDED.low, futures_quotes.low),
    close      = COALESCE(EXCLUDED.close, futures_quotes.close),
    average    = COALESCE(EXCLUDED.average, futures_quotes.average),
    volume     = COALESCE(EXCLUDED.volume, futures_quotes.volume),
    trades     = COALESCE(EXCLUDED.trades, futures_quotes.trades);

-- name: ListFuturesQuotesByRoot :many
SELECT c.symbol, c.expiration, c.multiplier, q.day, q.settlement, q.high, q.low, q.close, q.average, q.volume, q.trades
FROM futures_quotes q
JOIN futures_contracts c ON c.id = q.contract_id
WHERE c.root = $1 AND q.day >= $2 AND q.day <= $3
ORDER BY q.day, c.expiration;

-- name: CountFuturesQuotes :one
SELECT count(*) FROM futures_quotes
WHERE contract_id = $1;

-- name: LatestFuturesQuoteDay :one
SELECT day FROM futures_quotes q
JOIN futures_contracts c ON c.id = q.contract_id
WHERE c.root = $1
ORDER BY q.day DESC
LIMIT 1;
