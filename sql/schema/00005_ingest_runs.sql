-- +goose Up
CREATE TABLE ingest_runs (
    id          BIGSERIAL PRIMARY KEY,
    symbol_id   BIGINT NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
    timeframe   TEXT NOT NULL CHECK (timeframe IN ('5m', '1d')),
    range_start DATE NOT NULL,
    range_end   DATE NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('running', 'ok', 'empty', 'skipped', 'failed')),
    http_status INT,
    bars        INT NOT NULL DEFAULT 0 CHECK (bars >= 0),
    rejected    INT NOT NULL DEFAULT 0 CHECK (rejected >= 0),
    error       TEXT,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    duration_ms INT,
    CHECK (range_end >= range_start)
);

CREATE INDEX ingest_runs_symbol_idx ON ingest_runs (symbol_id, timeframe, started_at DESC);

CREATE INDEX ingest_runs_failed_idx ON ingest_runs (started_at DESC) WHERE status = 'failed';

-- +goose Down
DROP TABLE ingest_runs;
