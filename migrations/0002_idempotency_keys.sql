CREATE TABLE idempotency_keys (
    key TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    response_status INT,
    response_body JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_idempotency_keys_expires_at ON idempotency_keys(expires_at);

CREATE OR REPLACE FUNCTION cleanup_expired_idempotency_keys()
RETURNS void AS $$
BEGIN
    DELETE FROM idempotency_keys WHERE expires_at < NOW();
END;
$$ LANGUAGE plpgsql;
