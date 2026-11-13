-- Add tables for inventory reservations, checkout orders, regions, sales channels, and webhook subscriptions
-- Why: Replace remaining in-memory stores with PostgreSQL persistence
-- Reversible: yes

CREATE TABLE IF NOT EXISTS inventory_reservations (
    id TEXT PRIMARY KEY,
    variant_id TEXT NOT NULL,
    location_id TEXT NOT NULL,
    qty INT NOT NULL CHECK (qty > 0),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_inventory_reservations_expires_at ON inventory_reservations(expires_at);
CREATE INDEX IF NOT EXISTS idx_inventory_reservations_variant_loc ON inventory_reservations(variant_id, location_id);

CREATE TABLE IF NOT EXISTS checkout_orders (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    cart_id TEXT NOT NULL,
    status TEXT NOT NULL,
    total BIGINT NOT NULL CHECK (total >= 0),
    currency CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    payment_id TEXT NOT NULL DEFAULT '',
    reservations JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS regions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    currency CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    countries JSONB NOT NULL DEFAULT '[]'::jsonb,
    tax_rate_bps INT NOT NULL DEFAULT 0 CHECK (tax_rate_bps >= 0),
    tax_inclusive BOOLEAN NOT NULL DEFAULT false
);

INSERT INTO regions (id, name, currency, countries, tax_rate_bps, tax_inclusive) VALUES
    ('reg_ke', 'Kenya', 'KES', '["KE"]'::jsonb, 1600, true),
    ('reg_us', 'United States', 'USD', '["US"]'::jsonb, 800, false),
    ('reg_eu', 'Europe', 'EUR', '["DE","FR","NL"]'::jsonb, 2000, true)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS sales_channels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    regions JSONB NOT NULL DEFAULT '[]'::jsonb
);

INSERT INTO sales_channels (id, name, slug, regions) VALUES
    ('ch_web', 'Web Storefront', 'web', '["reg_ke","reg_us","reg_eu"]'::jsonb),
    ('ch_pos', 'Point of Sale', 'pos', '["reg_ke"]'::jsonb)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id TEXT PRIMARY KEY,
    url TEXT NOT NULL,
    secret TEXT NOT NULL,
    types JSONB NOT NULL DEFAULT '[]'::jsonb,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO schema_migrations (version) VALUES ('0008') ON CONFLICT (version) DO NOTHING;
