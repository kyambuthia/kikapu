-- Initialize catalog and orders schema
-- Why: Replace in-memory stores with Postgres persistence for products and orders
-- Reversible: no (requires manual backup/restore)

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Products table (catalog module)
CREATE TABLE products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_products_slug ON products(slug);
CREATE INDEX idx_products_status ON products(status);

-- Variants table (catalog module)
CREATE TABLE variants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    sku TEXT NOT NULL UNIQUE,
    price BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (price >= 0),
    CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE INDEX idx_variants_product_id ON variants(product_id);
CREATE INDEX idx_variants_sku ON variants(sku);

-- Product channels link table
CREATE TABLE product_channels (
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL,
    PRIMARY KEY (product_id, channel_id)
);

CREATE INDEX idx_product_channels_channel ON product_channels(channel_id);

-- Orders table (order module)
CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    number TEXT NOT NULL,
    region_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    payment_status TEXT NOT NULL DEFAULT 'pending',
    fulfillment_status TEXT NOT NULL DEFAULT 'unfulfilled',
    subtotal BIGINT NOT NULL,
    discount BIGINT NOT NULL DEFAULT 0,
    tax BIGINT NOT NULL,
    grand_total BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (region_id, number),
    CHECK (subtotal >= 0),
    CHECK (discount >= 0),
    CHECK (tax >= 0),
    CHECK (grand_total >= 0),
    CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE INDEX idx_orders_number ON orders(number);
CREATE INDEX idx_orders_region_id ON orders(region_id);
CREATE INDEX idx_orders_status ON orders(status);

-- Order lines table
CREATE TABLE order_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    variant_id TEXT NOT NULL,
    sku TEXT NOT NULL,
    quantity INT NOT NULL,
    unit_price BIGINT NOT NULL,
    discount BIGINT NOT NULL DEFAULT 0,
    tax BIGINT NOT NULL DEFAULT 0,
    currency CHAR(3) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (quantity > 0),
    CHECK (unit_price >= 0),
    CHECK (discount >= 0),
    CHECK (tax >= 0),
    CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE INDEX idx_order_lines_order_id ON order_lines(order_id);

-- Refunds table
CREATE TABLE refunds (
    id TEXT PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    amount BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (amount >= 0),
    CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE INDEX idx_refunds_order_id ON refunds(order_id);

-- Schema version tracking
CREATE TABLE schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO schema_migrations (version) VALUES ('0001');
