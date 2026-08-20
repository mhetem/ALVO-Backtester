-- +goose Up
CREATE TABLE brapi_usage (
    day      DATE PRIMARY KEY,
    requests INT NOT NULL DEFAULT 0 CHECK (requests >= 0)
);

-- +goose Down
DROP TABLE brapi_usage;
