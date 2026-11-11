# Database Migrations

This document describes the migration strategy and procedures for kikapu's PostgreSQL database.

## Overview

Kikapu uses forward-only migrations. Once a migration is applied to production, it cannot be undone in the normal flow. This approach:

- Eliminates reversion bugs
- Simplifies deployment procedures
- Ensures production state is always traceable forward
- Follows industry best practices (similar to Saleor, Shopify)

## Migration Naming Convention

All migration files must follow this naming pattern:

```
NNNN_description_in_snake_case.sql
```

Where:
- `NNNN` is a zero-padded sequence number (e.g., `0001`, `0012`, `0234`)
- `description` is a brief, lowercase, underscore-separated description
- `.sql` extension is required

Examples:
```
0001_create_products_table.sql
0002_add_sku_to_products.sql
0003_create_orders_table.sql
```

## Writing Migrations

### Forward-Only Policy

Every migration must:
1. Be forward-compatible (add columns, add tables, create indexes)
2. Include idempotency guards (IF NOT EXISTS, IF EXISTS)
3. Never drop or rename columns without explicit approval

### Idempotency Requirements

All migrations must be safe to re-run:

```sql
-- Correct: idempotent
CREATE TABLE IF NOT EXISTS products (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL
);

-- Wrong: will fail on re-run
CREATE TABLE products (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL
);
```

### Handling Destructive Changes

Destructive operations (DROP, RENAME) are not allowed by default. If absolutely necessary:

1. Add a special header comment at the top of the migration file:

```sql
-- allow-drop: reason_here
-- allow-drop: Link to incident or discussion: https://github.com/kyambuthia/kikapu/issues/XXX

ALTER TABLE products DROP COLUMN deprecated_field;
```

2. The migration gate will detect and validate this header
3. If the header is missing, the gate will fail with a clear error message

### Example Migration

```sql
-- 0005_add_inventory_tracking.sql
-- Track stock levels per variant/warehouse

CREATE TABLE IF NOT EXISTS inventory (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    variant_id TEXT NOT NULL,
    warehouse_id TEXT NOT NULL,
    on_hand BIGINT NOT NULL DEFAULT 0,
    reserved BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(variant_id, warehouse_id)
);

CREATE INDEX IF NOT EXISTS idx_inventory_variant ON inventory(variant_id);
CREATE INDEX IF NOT EXISTS idx_inventory_warehouse ON inventory(warehouse_id);
```

## Migration Gate

The migration compatibility gate (`scripts/migration-gate.sh`) validates:

1. **Idempotency**: All migrations apply exactly once on re-run
2. **Data Preservation**: Row counts remain unchanged after new migrations
3. **No Undeclared Destructive Changes**: DROP/RENAME must have `allow-drop` header

### Running Locally

```bash
# With Docker (default)
./scripts/migration-gate.sh

# With existing Postgres
export TEST_DATABASE_URL=postgres://user:pass@localhost:5432/kikapu
./scripts/migration-gate.sh
```

### Self-Test

The gate includes a self-test that injects a destructive change:

```bash
./scripts/migration-gate.sh --self-test
```

This should fail with a clear error message about the unauthorized DROP statement.

## CI Integration

The migration gate runs automatically in CI via `.github/workflows/migrations.yml`:

- Triggers on PRs that modify `migrations/`
- Uses a throwaway PostgreSQL service container
- Validates against the previous release tag
- Blocks merge if checks fail

## Deployment Procedure

### Normal Deployment

1. Create migration file in `migrations/`
2. Run migration gate locally: `./scripts/migration-gate.sh`
3. Commit and create PR
4. CI validates automatically
5. Merge only after gate passes
6. Deploy to production
7. Monitor for errors

### Handling Migration Failures

If a migration fails in production:

1. **Never revert the migration**
2. Create a new forward-fix migration:
   ```sql
   -- 0023_fix_previous_migration.sql
   -- Fix missing index from migration 0022
   
   CREATE INDEX IF NOT EXISTS idx_orders_customer ON orders(customer_id);
   ```
3. Apply forward-fix via normal deployment

## Reversible Pairs (Advanced)

In rare cases, a reversible migration pair may be required:

```sql
-- 0010_add_email_to_users.up.sql
ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT;

-- 0010_add_email_to_users.down.sql (for emergency rollback only)
-- WARNING: This file is for emergency use only
ALTER TABLE users DROP COLUMN IF EXISTS email;
```

Files with `.down.sql` suffix:
- Are **not** executed during normal deployment
- Must be approved by two team members
- Must be documented with incident ID
- Must be tested in staging environment first

## Emergency Rollback Procedure

**Only use in production incidents with documented approval.**

1. **Authorization Required**:
   - Incident commander approval
   - Two senior engineer sign-offs
   - Documented incident ID

2. **Procedure**:
   ```bash
   # 1. Stop application servers
   systemctl stop kikapu
   
   # 2. Take database backup (if not already)
   pg_dump $DATABASE_URL > emergency_backup_$(date +%Y%m%d_%H%M%S).sql
   
   # 3. Apply .down.sql file manually
   psql $DATABASE_URL -f migrations/0010_add_email_to_users.down.sql
   
   # 4. Verify data integrity
   psql $DATABASE_URL -c "SELECT COUNT(*) FROM users;"
   
   # 5. Document rollback
   echo "Rollback applied: 0010_add_email_to_users.down.sql at $(date)" >> ROLLBACK_LOG
   
   # 6. Start application
   systemctl start kikapu
   
   # 7. Monitor and create forward-fix migration
   ```

3. **Post-Rollback**:
   - Create post-mortem document
   - Add `-- allow-drop` header to `.up.sql` or rewrite migration
   - Test thoroughly in staging before next deployment

## Best Practices

### Do's

- ✅ Use `IF NOT EXISTS` for CREATE statements
- ✅ Use `IF EXISTS` for DROP statements (when authorized)
- ✅ Add comments explaining the purpose
- ✅ Include index creation separately from table creation
- ✅ Use transactions for multi-statement migrations
- ✅ Test locally before committing

### Don'ts

- ❌ Never use DROP without `allow-drop` header
- ❌ Never alter column types without approval
- ❌ Never rename tables or columns without approval
- ❌ Never assume tables/columns exist (use IF EXISTS)
- ❌ Never include data modifications in schema migrations

### Performance Considerations

- Create indexes concurrently in production: `CREATE INDEX CONCURRENTLY`
- Avoid locking tables during peak hours
- Large table modifications should be batched
- Monitor migration execution time

## Schema Management

### Creating a New Table

```sql
-- 0008_create_categories_table.sql
CREATE TABLE IF NOT EXISTS categories (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    parent_id TEXT REFERENCES categories(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_categories_parent ON categories(parent_id);
CREATE INDEX IF NOT EXISTS idx_categories_slug ON categories(slug);
```

### Adding a Column

```sql
-- 0011_add_metadata_to_products.sql
ALTER TABLE products ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_products_metadata ON products USING GIN(metadata);
```

### Creating an Index

```sql
-- 0012_add_order_status_index.sql
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status) 
WHERE status IN ('pending', 'confirmed');
```

## Testing Migrations

### Local Testing

```bash
# Run with Docker
./scripts/migration-gate.sh

# Run with existing database
export TEST_DATABASE_URL=postgres://localhost/kikapu_test
./scripts/migration-gate.sh
```

### Manual Testing

```bash
# Create a test database
createdb kikapu_migration_test

# Apply previous migrations
for f in $(ls migrations/*.sql | sort); do
    psql kikapu_migration_test -f "$f"
done

# Verify schema
psql kikapu_migration_test -c "\dt"

# Clean up
dropdb kikapu_migration_test
```

## Frequent Issues

### "Migration already applied"

This is **expected behavior** for idempotent migrations. The gate checks if the migration was already applied and skips it.

### "No previous tags found"

You need at least one git tag before running the migration gate:

```bash
git tag v0.1.0
git push origin v0.1.0
```

### "Destructive change detected"

Add the required header:

```sql
-- allow-drop: Reason for destructive change
-- allow-drop: Incident: https://github.com/kyambuthia/kikapu/issues/XXX

ALTER TABLE products DROP COLUMN deprecated_field;
```

### "Row count changed"

This indicates a migration deleted or modified seeded data. Review the migration for unintended data modifications.

## Resources

- [Saleor Migration Compatibility](https://github.com/saleor/saleor/blob/main/saleor/tests/test_migrations_compatibility.py)
- [Schema Migration Best Practices](https://www.brunospinelli.dev/posts/schema-migrations-best-practices/)
- [PostgreSQL Migration Tools](https://www.postgresql.org/docs/current/migration.html)

## Questions?

For questions or concerns about migrations:
- Open an issue: https://github.com/kyambuthia/kikapu/issues
- Check existing docs: [docs/](https://github.com/kyambuthia/kikapu/tree/main/docs)
