-- Add full-text search support for products
-- Why: Enable fast, ranked product search without external search engine dependencies
-- Reversible: yes

-- Enable pg_trgm extension for typo tolerance
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Add search tsvector column to products
ALTER TABLE products ADD COLUMN search tsvector;

-- Create GIN index on search column for fast full-text queries
CREATE INDEX idx_products_search ON products USING GIN(search);

-- Create GIN trigram index on name for typo tolerance
CREATE INDEX idx_products_name_trgm ON products USING GIN(name gin_trgm_ops);

-- Function to update search tsvector
CREATE OR REPLACE FUNCTION update_product_search_tsvector()
RETURNS TRIGGER AS $$
BEGIN
    NEW.search := 
        setweight(to_tsvector('english', COALESCE(NEW.name, '')), 'A') ||
        setweight(to_tsvector('english', COALESCE(NEW.slug, '')), 'B');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to auto-update search column on insert or update
CREATE TRIGGER products_search_update
    BEFORE INSERT OR UPDATE ON products
    FOR EACH ROW
    EXECUTE FUNCTION update_product_search_tsvector();

-- Update existing products
UPDATE products SET search = 
    setweight(to_tsvector('english', COALESCE(name, '')), 'A') ||
    setweight(to_tsvector('english', COALESCE(slug, '')), 'B');

-- Function to aggregate variant SKUs into product search
-- This trigger updates the product search when variants change
CREATE OR REPLACE FUNCTION update_product_search_from_variants()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE products p
    SET search = 
        setweight(to_tsvector('english', COALESCE(p.name, '')), 'A') ||
        setweight(to_tsvector('english', COALESCE(p.slug, '')), 'B') ||
        setweight(
            (SELECT to_tsvector('english', string_agg(COALESCE(v.sku, ''), ' '))
             FROM variants v
             WHERE v.product_id = p.id),
            'C'
        )
    WHERE p.id = COALESCE(NEW.product_id, OLD.product_id);
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

-- Trigger on variants to update product search
CREATE TRIGGER variants_search_update
    AFTER INSERT OR UPDATE OR DELETE ON variants
    FOR EACH ROW
    EXECUTE FUNCTION update_product_search_from_variants();

-- Populate search with existing variant SKUs
UPDATE products p
SET search = 
    setweight(to_tsvector('english', COALESCE(p.name, '')), 'A') ||
    setweight(to_tsvector('english', COALESCE(p.slug, '')), 'B') ||
    setweight(
        (SELECT to_tsvector('english', string_agg(COALESCE(v.sku, ''), ' '))
         FROM variants v
         WHERE v.product_id = p.id),
        'C'
    );

INSERT INTO schema_migrations (version) VALUES ('0004');
