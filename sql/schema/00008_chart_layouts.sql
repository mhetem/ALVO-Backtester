-- +goose Up
CREATE TABLE chart_layouts (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    symbol_id  BIGINT NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
    layout     JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, symbol_id)
);

-- +goose Down
DROP TABLE chart_layouts;
