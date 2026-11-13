-- Add idempotency keys table for mutation replay protection
-- Why: Persist idempotency responses across restarts (INV-2)
-- Reversible: no

CREATE TABLE IF NOT EXISTS idempotency_keys (
    key_hash BYTEA PRIMARY KEY,
    status_code INT NOT NULL,
    response_body BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_created_at ON idempotency_keys(created_at);

INSERT INTO schema_migrations (version) VALUES ('0002') ON CONFLICT (version) DO NOTHING;
