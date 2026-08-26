-- +goose Up
ALTER TABLE backtest_trades
    ADD COLUMN dividends_cents BIGINT NOT NULL DEFAULT 0;

ALTER TABLE backtest_equity
    ADD COLUMN hold_cents BIGINT,
    ADD COLUMN index_cents BIGINT;

-- +goose Down
ALTER TABLE backtest_equity
    DROP COLUMN index_cents,
    DROP COLUMN hold_cents;

ALTER TABLE backtest_trades
    DROP COLUMN dividends_cents;
