-- +goose Up
CREATE TABLE backtest_run_symbols (
    run_id    UUID NOT NULL REFERENCES backtest_runs(id) ON DELETE CASCADE,
    ord       INT NOT NULL CHECK (ord >= 0),
    symbol_id BIGINT NOT NULL REFERENCES symbols(id),
    PRIMARY KEY (run_id, ord),
    UNIQUE (run_id, symbol_id)
);

ALTER TABLE backtest_runs
    ADD COLUMN max_positions INT NOT NULL DEFAULT 1 CHECK (max_positions > 0);

ALTER TABLE backtest_trades
    ADD COLUMN symbol_id BIGINT REFERENCES symbols(id);

UPDATE backtest_trades t
SET symbol_id = r.symbol_id
FROM backtest_runs r
WHERE r.id = t.run_id;

ALTER TABLE backtest_trades
    ALTER COLUMN symbol_id SET NOT NULL;

INSERT INTO backtest_run_symbols (run_id, ord, symbol_id)
SELECT id, 0, symbol_id FROM backtest_runs;

-- +goose Down
ALTER TABLE backtest_trades
    DROP COLUMN symbol_id;

ALTER TABLE backtest_runs
    DROP COLUMN max_positions;

DROP TABLE backtest_run_symbols;
