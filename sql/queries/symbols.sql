-- name: UpsertSymbol :one
INSERT INTO symbols (
    ticker, short_name, long_name, kind, currency,
    lot_size, tick_size, point_value, active, tracked,
    first_seen, last_seen
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12
)
ON CONFLICT (ticker) DO UPDATE SET
    short_name  = COALESCE(EXCLUDED.short_name, symbols.short_name),
    long_name   = COALESCE(EXCLUDED.long_name, symbols.long_name),
    kind        = EXCLUDED.kind,
    currency    = EXCLUDED.currency,
    lot_size    = EXCLUDED.lot_size,
    tick_size   = EXCLUDED.tick_size,
    point_value = COALESCE(EXCLUDED.point_value, symbols.point_value),
    active      = EXCLUDED.active,
    tracked     = symbols.tracked OR EXCLUDED.tracked,
    first_seen  = LEAST(symbols.first_seen, EXCLUDED.first_seen),
    last_seen   = GREATEST(symbols.last_seen, EXCLUDED.last_seen),
    updated_at  = NOW()
RETURNING *;

-- name: GetSymbolByTicker :one
SELECT * FROM symbols WHERE ticker = $1;

-- name: ListSymbols :many
SELECT * FROM symbols ORDER BY ticker;

-- name: SearchSymbols :many
SELECT *
FROM symbols
WHERE ($1::text = ''
       OR strpos(ticker, upper($1::text)) > 0
       OR strpos(upper(long_name), upper($1::text)) > 0)
  AND ($2::text = '' OR kind = $2::text)
ORDER BY
    (strpos(ticker, upper($1::text)) = 1) DESC,
    (strpos(ticker, upper($1::text)) > 0) DESC,
    tracked DESC,
    active DESC,
    ticker
LIMIT $3;

-- name: ListTrackedSymbols :many
SELECT * FROM symbols WHERE tracked ORDER BY ticker;

-- name: DeactivateSymbols :many
UPDATE symbols
SET active = FALSE, updated_at = NOW()
WHERE active AND ticker = ANY($1::text[])
RETURNING ticker;
