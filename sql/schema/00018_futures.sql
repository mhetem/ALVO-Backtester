-- +goose Up
CREATE TABLE futures_contracts (
    id          BIGSERIAL PRIMARY KEY,
    symbol      TEXT NOT NULL UNIQUE,
    root        TEXT NOT NULL,
    description TEXT,
    segment     TEXT,
    multiplier  NUMERIC(18,6) NOT NULL CHECK (multiplier > 0),
    lot_size    INT NOT NULL DEFAULT 1 CHECK (lot_size > 0),
    currency    TEXT NOT NULL DEFAULT 'BRL',
    isin        TEXT,
    first_trade DATE,
    last_trade  DATE,
    expiration  DATE NOT NULL,
    seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX futures_contracts_root_idx ON futures_contracts (root, expiration);

CREATE TABLE futures_quotes (
    contract_id BIGINT NOT NULL REFERENCES futures_contracts(id) ON DELETE CASCADE,
    day         DATE NOT NULL,
    settlement  NUMERIC(18,6) NOT NULL CHECK (settlement > 0),
    high        NUMERIC(18,6) CHECK (high > 0),
    low         NUMERIC(18,6) CHECK (low > 0),
    close       NUMERIC(18,6) CHECK (close > 0),
    average     NUMERIC(18,6) CHECK (average > 0),
    volume      BIGINT CHECK (volume >= 0),
    trades      BIGINT CHECK (trades >= 0),
    PRIMARY KEY (contract_id, day),
    CHECK (high IS NULL OR low IS NULL OR high >= low),
    CHECK (close IS NULL OR low IS NULL OR close >= low),
    CHECK (close IS NULL OR high IS NULL OR close <= high)
);

CREATE INDEX futures_quotes_day_idx ON futures_quotes (day);

-- +goose Down
DROP TABLE futures_quotes;
DROP TABLE futures_contracts;
