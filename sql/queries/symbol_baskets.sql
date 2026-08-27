-- name: ListSymbolBaskets :many
SELECT * FROM symbol_baskets
WHERE user_id = $1
ORDER BY name;

-- name: CountSymbolBaskets :one
SELECT COUNT(*) FROM symbol_baskets
WHERE user_id = $1;

-- name: GetSymbolBasket :one
SELECT * FROM symbol_baskets
WHERE id = $1 AND user_id = $2;

-- name: CreateSymbolBasket :one
INSERT INTO symbol_baskets (user_id, name)
VALUES ($1, $2)
RETURNING *;

-- name: UpdateSymbolBasket :one
UPDATE symbol_baskets
SET name = $3, updated_at = NOW()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteSymbolBasket :execrows
DELETE FROM symbol_baskets
WHERE id = $1 AND user_id = $2;

-- name: CreateSymbolBasketSymbols :copyfrom
INSERT INTO symbol_basket_symbols (basket_id, ord, symbol_id)
VALUES ($1, $2, $3);

-- name: DeleteSymbolBasketSymbols :exec
DELETE FROM symbol_basket_symbols
WHERE basket_id = $1;

-- name: ListSymbolBasketMembers :many
SELECT bs.basket_id, bs.ord, s.*
FROM symbol_basket_symbols bs
JOIN symbols s ON s.id = bs.symbol_id
WHERE bs.basket_id = ANY($1::uuid[])
ORDER BY bs.basket_id, bs.ord;
