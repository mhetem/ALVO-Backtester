-- +goose Up
CREATE TABLE symbol_baskets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, name)
);

CREATE INDEX symbol_baskets_user_idx ON symbol_baskets (user_id);

CREATE TABLE symbol_basket_symbols (
    basket_id UUID NOT NULL REFERENCES symbol_baskets(id) ON DELETE CASCADE,
    ord       INT NOT NULL CHECK (ord >= 0),
    symbol_id BIGINT NOT NULL REFERENCES symbols(id),
    PRIMARY KEY (basket_id, ord),
    UNIQUE (basket_id, symbol_id)
);

-- +goose Down
DROP TABLE symbol_basket_symbols;
DROP TABLE symbol_baskets;
