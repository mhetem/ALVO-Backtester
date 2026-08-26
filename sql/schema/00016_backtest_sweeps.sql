-- +goose Up
CREATE TABLE backtest_sweeps (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    strategy_id   UUID NOT NULL REFERENCES strategies(id),
    kind          TEXT NOT NULL CHECK (kind IN ('grid', 'walk_forward')),
    spec          JSONB NOT NULL,
    axes          JSONB NOT NULL,
    folds         JSONB,
    objective     TEXT NOT NULL,
    symbol_id     BIGINT NOT NULL REFERENCES symbols(id),
    timeframe     TEXT NOT NULL CHECK (timeframe IN ('5m', '15m', '30m', '1h', '1d')),
    start_date    DATE NOT NULL,
    end_date      DATE NOT NULL,
    capital_cents BIGINT NOT NULL CHECK (capital_cents > 0),
    max_positions INT NOT NULL DEFAULT 1 CHECK (max_positions > 0),
    points        INT NOT NULL CHECK (points > 0),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (end_date >= start_date)
);

CREATE INDEX backtest_sweeps_user_idx ON backtest_sweeps (user_id, created_at DESC);

CREATE TABLE backtest_sweep_symbols (
    sweep_id  UUID NOT NULL REFERENCES backtest_sweeps(id) ON DELETE CASCADE,
    ord       INT NOT NULL CHECK (ord >= 0),
    symbol_id BIGINT NOT NULL REFERENCES symbols(id),
    PRIMARY KEY (sweep_id, ord),
    UNIQUE (sweep_id, symbol_id)
);

ALTER TABLE backtest_runs
    ADD COLUMN sweep_id UUID REFERENCES backtest_sweeps(id) ON DELETE CASCADE,
    ADD COLUMN params JSONB,
    ADD COLUMN point INT,
    ADD COLUMN fold INT,
    ADD COLUMN phase TEXT CHECK (phase IN ('in_sample', 'out_of_sample'));

CREATE INDEX backtest_runs_sweep_idx ON backtest_runs (sweep_id, fold) WHERE sweep_id IS NOT NULL;

CREATE UNIQUE INDEX backtest_runs_fold_out_idx
    ON backtest_runs (sweep_id, fold)
    WHERE phase = 'out_of_sample';

DROP INDEX backtest_runs_queue_idx;

CREATE INDEX backtest_runs_queue_idx
    ON backtest_runs ((sweep_id IS NOT NULL), created_at)
    WHERE status = 'queued';

-- +goose Down
DROP INDEX backtest_runs_queue_idx;

CREATE INDEX backtest_runs_queue_idx ON backtest_runs (created_at) WHERE status = 'queued';

DROP INDEX backtest_runs_fold_out_idx;

DROP INDEX backtest_runs_sweep_idx;

ALTER TABLE backtest_runs
    DROP COLUMN phase,
    DROP COLUMN fold,
    DROP COLUMN point,
    DROP COLUMN params,
    DROP COLUMN sweep_id;

DROP TABLE backtest_sweep_symbols;

DROP TABLE backtest_sweeps;
