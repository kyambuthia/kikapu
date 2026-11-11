-- Add admin-specific tables for idempotency and stock levels
-- Why: Support admin API with idempotent mutations and per-location stock tracking
-- Reversible: no

-- Idempotency keys table for ensuring exactly-once mutations
CREATE TABLE idempotency_keys (
    key_hash BYTEA PRIMARY KEY,
    status_code INT NOT NULL,
    response_body BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_idempotency_keys_created_at ON idempotency_keys(created_at);

-- Stock levels table for per-location inventory tracking
CREATE TABLE stock_levels (
    variant_id UUID NOT NULL REFERENCES variants(id) ON DELETE CASCADE,
    location_id TEXT NOT NULL,
    on_hand INT NOT NULL DEFAULT 0,
    reserved INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (variant_id, location_id),
    CHECK (on_hand >= 0),
    CHECK (reserved >= 0),
    CHECK (reserved <= on_hand)
);

CREATE INDEX idx_stock_levels_variant_id ON stock_levels(variant_id);
CREATE INDEX idx_stock_levels_location_id ON stock_levels(location_id);

-- Update products table to support archived status
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_status_check;
ALTER TABLE products ADD CONSTRAINT products_status_check 
    CHECK (status IN ('draft', 'published', 'archived'));

INSERT INTO schema_migrations (version) VALUES ('0002');
