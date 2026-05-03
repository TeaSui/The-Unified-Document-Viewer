-- Combined init script for Docker Compose (runs both migrations)
CREATE TABLE IF NOT EXISTS audit_request (
    request_id  TEXT PRIMARY KEY,
    vin         TEXT NOT NULL,
    ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
    http_status INT NOT NULL,
    duration_ms INT NOT NULL,
    outcomes    JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_vin_ts ON audit_request(vin, ts DESC);

CREATE TABLE IF NOT EXISTS cached_response (
    vin        TEXT NOT NULL,
    source     TEXT NOT NULL,
    payload    JSONB NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (vin, source)
);

CREATE INDEX IF NOT EXISTS idx_cached_fetched ON cached_response(fetched_at);
