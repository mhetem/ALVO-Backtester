-- +goose Up
ALTER TABLE strategies
    ADD COLUMN share_token TEXT UNIQUE,
    ADD COLUMN shared_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE strategies
    DROP COLUMN shared_at,
    DROP COLUMN share_token;
