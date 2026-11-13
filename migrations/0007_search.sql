-- Add full-text search support for products
-- Why: Enable fast, ranked product search without external search engine dependencies
-- Reversible: yes

CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE products ADD COLUMN IF NOT EXISTS search tsvector;

CREATE INDEX IF NOT EXISTS idx_products_search ON products USING GIN(search);
CREATE INDEX IF NOT EXISTS idx_products_name_trgm ON products USING GIN(name gin_trgm_ops);

CREATE OR REPLACE FUNCTION update_product_search_tsvector()
RETURNS TRIGGER AS $$
BEGIN
    NEW.search :=
        setweight(to_tsvector('english', COALESCE(NEW.name, '')), 'A') ||
        setweight(to_tsvector('english', COALESCE(NEW.slug, '')), 'B');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS products_search_update ON products;
CREATE TRIGGER products_search_update
    BEFORE INSERT OR UPDATE OF name, slug ON products
    FOR EACH ROW
    EXECUTE FUNCTION update_product_search_tsvector();

UPDATE products SET search =
    setweight(to_tsvector('english', COALESCE(name, '')), 'A') ||
    setweight(to_tsvector('english', COALESCE(slug, '')), 'B');

CREATE OR REPLACE FUNCTION update_product_search_from_variants()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE products p
    SET search =
        setweight(to_tsvector('english', COALESCE(p.name, '')), 'A') ||
        setweight(to_tsvector('english', COALESCE(p.slug, '')), 'B') ||
        setweight(
            to_tsvector('english', COALESCE((SELECT string_agg(COALESCE(v.sku, ''), ' ')
             FROM variants v
             WHERE v.product_id = p.id), '')),
            'C'
        )
    WHERE p.id = COALESCE(NEW.product_id, OLD.product_id);
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS variants_search_update ON variants;
CREATE TRIGGER variants_search_update
    AFTER INSERT OR UPDATE OR DELETE ON variants
    FOR EACH ROW
    EXECUTE FUNCTION update_product_search_from_variants();

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

INSERT INTO schema_migrations (version) VALUES ('0007') ON CONFLICT (version) DO NOTHING;
