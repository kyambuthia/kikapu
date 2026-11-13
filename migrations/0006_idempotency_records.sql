-- Add general-purpose idempotency_records table for API mutations
-- Why: Support idempotent mutations across all API endpoints with conflict detection (INV-2)
-- Reversible: no

CREATE TABLE IF NOT EXISTS idempotency_records (
    key_hash BYTEA PRIMARY KEY,
    request_hash BYTEA NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'done')),
    response_status INT,
    response_body BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '24 hours')
);

CREATE INDEX IF NOT EXISTS idx_idempotency_records_expires_at ON idempotency_records(expires_at);
CREATE INDEX IF NOT EXISTS idx_idempotency_records_status ON idempotency_records(status);

INSERT INTO schema_migrations (version) VALUES ('0006') ON CONFLICT (version) DO NOTHING;
