-- +goose Up
CREATE TABLE candles (
    symbol_id BIGINT NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
    timeframe TEXT NOT NULL CHECK (timeframe IN ('5m', '1d')),
    ts        TIMESTAMPTZ NOT NULL,
    open      NUMERIC(18,6) NOT NULL CHECK (open > 0),
    high      NUMERIC(18,6) NOT NULL CHECK (high > 0),
    low       NUMERIC(18,6) NOT NULL CHECK (low > 0),
    close     NUMERIC(18,6) NOT NULL CHECK (close > 0),
    adj_close NUMERIC(18,6) CHECK (adj_close > 0),
    volume    BIGINT NOT NULL CHECK (volume >= 0),
    PRIMARY KEY (symbol_id, timeframe, ts),
    CHECK (high >= low),
    CHECK (high >= open AND high >= close),
    CHECK (low <= open AND low <= close)
);

-- +goose Down
DROP TABLE candles;
