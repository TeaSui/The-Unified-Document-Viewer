-- +goose Up
CREATE TABLE cached_response (
    vin        TEXT NOT NULL,
    source     TEXT NOT NULL,
    payload    JSONB NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (vin, source)
);

CREATE INDEX idx_cached_fetched ON cached_response(fetched_at);

-- +goose Down
DROP TABLE IF EXISTS cached_response;
