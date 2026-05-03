-- +goose Up
CREATE TABLE audit_request (
    request_id  TEXT PRIMARY KEY,
    vin         TEXT NOT NULL,
    ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
    http_status INT NOT NULL,
    duration_ms INT NOT NULL,
    outcomes    JSONB NOT NULL
);

CREATE INDEX idx_audit_vin_ts ON audit_request(vin, ts DESC);

-- +goose Down
DROP TABLE IF EXISTS audit_request;
