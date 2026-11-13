-- Add stock_levels table for inventory tracking
-- Why: Persist stock on_hand and reserved quantities per variant/location (INV-3)
-- Reversible: no

CREATE TABLE IF NOT EXISTS stock_levels (
    variant_id TEXT NOT NULL,
    location_id TEXT NOT NULL,
    on_hand INT NOT NULL DEFAULT 0,
    reserved INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (variant_id, location_id),
    CHECK (on_hand >= 0),
    CHECK (reserved >= 0),
    CHECK (on_hand >= reserved)
);

CREATE INDEX IF NOT EXISTS idx_stock_levels_variant ON stock_levels(variant_id);
CREATE INDEX IF NOT EXISTS idx_stock_levels_location ON stock_levels(location_id);

INSERT INTO schema_migrations (version) VALUES ('0003') ON CONFLICT (version) DO NOTHING;
