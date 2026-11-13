-- Add carts, outbox_events, and api_keys tables for Postgres persistence
-- Why: Persist carts, transactional outbox events, and API keys in PostgreSQL
-- Reversible: no

CREATE TABLE IF NOT EXISTS carts (
    id TEXT PRIMARY KEY,
    currency CHAR(3) NOT NULL,
    lines JSONB NOT NULL DEFAULT '[]'::jsonb,
    version INT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (currency ~ '^[A-Z]{3}$'),
    CHECK (version >= 1)
);

CREATE TABLE IF NOT EXISTS outbox_events (
    id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    event_version INT NOT NULL DEFAULT 1,
    aggregate_id TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    data JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INT NOT NULL DEFAULT 0,
    next_attempt TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (status IN ('pending', 'sent', 'dead_letter'))
);

CREATE INDEX IF NOT EXISTS idx_outbox_events_claim ON outbox_events(status, next_attempt, created_at);

CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    prefix TEXT NOT NULL,
    hash BYTEA NOT NULL UNIQUE,
    scopes TEXT[] NOT NULL,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO schema_migrations (version) VALUES ('0005') ON CONFLICT (version) DO NOTHING;
