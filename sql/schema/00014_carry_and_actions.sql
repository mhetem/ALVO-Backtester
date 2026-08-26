-- +goose Up
ALTER TABLE backtest_trades
    ADD COLUMN borrow_cents BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN split_cash_cents BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE backtest_trades
    DROP COLUMN split_cash_cents,
    DROP COLUMN borrow_cents;
