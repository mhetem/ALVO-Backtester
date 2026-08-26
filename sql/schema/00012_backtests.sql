-- +goose Up
CREATE TABLE backtest_runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    strategy_id   UUID NOT NULL REFERENCES strategies(id),
    spec          JSONB NOT NULL,
    symbol_id     BIGINT NOT NULL REFERENCES symbols(id),
    timeframe     TEXT NOT NULL CHECK (timeframe IN ('5m', '15m', '30m', '1h', '1d')),
    start_date    DATE NOT NULL,
    end_date      DATE NOT NULL,
    capital_cents BIGINT NOT NULL CHECK (capital_cents > 0),
    status        TEXT NOT NULL CHECK (status IN ('queued', 'running', 'done', 'error')),
    metrics       JSONB,
    error         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    CHECK (end_date >= start_date)
);

CREATE INDEX backtest_runs_queue_idx ON backtest_runs (created_at) WHERE status = 'queued';

CREATE INDEX backtest_runs_user_idx ON backtest_runs (user_id, created_at DESC);

CREATE TABLE backtest_trades (
    run_id      UUID NOT NULL REFERENCES backtest_runs(id) ON DELETE CASCADE,
    seq         INT NOT NULL CHECK (seq > 0),
    side        TEXT NOT NULL CHECK (side IN ('long', 'short')),
    qty         BIGINT NOT NULL CHECK (qty > 0),
    entry_ts    TIMESTAMPTZ NOT NULL,
    entry_price NUMERIC(18,6) NOT NULL CHECK (entry_price > 0),
    exit_ts     TIMESTAMPTZ,
    exit_price  NUMERIC(18,6) CHECK (exit_price > 0),
    pnl_cents   BIGINT,
    fees_cents  BIGINT NOT NULL CHECK (fees_cents >= 0),
    exit_reason TEXT,
    PRIMARY KEY (run_id, seq)
);

CREATE TABLE backtest_equity (
    run_id       UUID NOT NULL REFERENCES backtest_runs(id) ON DELETE CASCADE,
    ts           TIMESTAMPTZ NOT NULL,
    equity_cents BIGINT NOT NULL,
    PRIMARY KEY (run_id, ts)
);

-- +goose Down
DROP TABLE backtest_equity;

DROP TABLE backtest_trades;

DROP TABLE backtest_runs;
