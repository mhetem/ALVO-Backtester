-- +goose Up
CREATE TABLE symbols (
    id          BIGSERIAL PRIMARY KEY,
    ticker      TEXT NOT NULL UNIQUE,
    short_name  TEXT,
    long_name   TEXT,
    kind        TEXT NOT NULL CHECK (kind IN ('stock', 'fii', 'bdr', 'unit', 'index', 'future', 'crypto')),
    currency    TEXT NOT NULL DEFAULT 'BRL',
    lot_size    INT NOT NULL DEFAULT 100 CHECK (lot_size > 0),
    tick_size   NUMERIC(18,8) NOT NULL DEFAULT 0.01 CHECK (tick_size > 0),
    point_value NUMERIC(18,8) CHECK (point_value > 0),
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    tracked     BOOLEAN NOT NULL DEFAULT FALSE,
    first_seen  DATE,
    last_seen   DATE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX symbols_tracked_idx ON symbols (ticker) WHERE tracked;

CREATE INDEX symbols_kind_idx ON symbols (kind);

-- +goose Down
DROP TABLE symbols;
