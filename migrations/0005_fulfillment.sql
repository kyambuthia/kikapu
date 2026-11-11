-- Add fulfillments table for order fulfillment tracking
-- Why: Support pick, pack, ship workflow with tracking information
-- Reversible: no

CREATE TABLE fulfillments (
    id TEXT PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending',
    carrier TEXT,
    tracking_number TEXT,
    tracking_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (status IN ('pending', 'picked', 'packed', 'shipped', 'delivered', 'cancelled')),
    CHECK (tracking_url = '' OR tracking_url IS NULL OR tracking_url LIKE 'https://%')
);

CREATE INDEX idx_fulfillments_order_id ON fulfillments(order_id);
CREATE INDEX idx_fulfillments_status ON fulfillments(status);

CREATE TABLE fulfillment_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fulfillment_id TEXT NOT NULL REFERENCES fulfillments(id) ON DELETE CASCADE,
    variant_id TEXT NOT NULL,
    quantity INT NOT NULL,
    CHECK (quantity > 0)
);

CREATE INDEX idx_fulfillment_items_fulfillment_id ON fulfillment_items(fulfillment_id);
CREATE INDEX idx_fulfillment_items_variant_id ON fulfillment_items(variant_id);

INSERT INTO schema_migrations (version) VALUES ('0005');
