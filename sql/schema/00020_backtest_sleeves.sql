-- +goose Up
CREATE TABLE backtest_symbol_equity (
    run_id       UUID NOT NULL REFERENCES backtest_runs(id) ON DELETE CASCADE,
    symbol_id    BIGINT NOT NULL REFERENCES symbols(id),
    ts           TIMESTAMPTZ NOT NULL,
    equity_cents BIGINT NOT NULL,
    PRIMARY KEY (run_id, symbol_id, ts)
);

-- +goose Down
DROP TABLE backtest_symbol_equity;
