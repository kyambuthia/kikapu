# Database Migrations

This document describes the migration strategy, naming conventions, and procedures for the kikapu project.

## Overview

Kikapu uses forward-only SQL migrations stored in the `migrations/` directory. The migration system:

- Uses PostgreSQL as the database backend
- Applies migrations with advisory locking to prevent concurrent runs
- Tracks applied migrations in a `schema_migrations` table
- Runs in CI via the Migration Compatibility Gate

## Migration Strategy

### Forward-Only Policy

Kikapu follows a **forward-only migration policy**. Once a migration is applied:

1. **Never modify** an existing migration file
2. **Never delete** a migration that has been applied to production
3. **Always create a new migration** to change or fix schema

This approach ensures:

- Reproducible deployments across environments
- No risk of data loss from automated rollbacks
- Clear audit trail of schema changes

### When Reversible Pairs Are Required

While the policy is forward-only, some operations require reversible pairs for safety:

#### Recommended Reversible Operations

1. **Adding a new table or column**: Safe to add, but consider a rollback if unsure about the data model
2. **Creating an index**: Use `CONCURRENTLY` to avoid blocking, and have a rollback plan
3. **Adding a constraint**: Add as `NOT VALID` first, validate separately

#### Example: Safe Column Addition

```sql
-- Migration 1: Add column
-- migrations/0002_add_email_column.sql
ALTER TABLE users ADD COLUMN email TEXT;
```

Reversible pair (if needed for emergency):
```sql
-- Migration 3: Remove column (emergency only)
-- migrations/0003_remove_email_column.sql
-- allow-drop: Production issue #1234

ALTER TABLE users DROP COLUMN email;
```

## Naming Convention

Migration files follow this pattern:

```
NNNN_description.sql
```

Where:
- `NNNN`: 4-digit zero-padded version number (e.g., `0001`, `0002`)
- `description`: Brief snake_case description of the change

Examples:
- `0001_init.sql`
- `0002_add_email_column.sql`
- `0003_create_order_index.sql`

## Migration File Structure

Every migration file should include:

1. **Header comment** explaining the change
2. **Why section** describing the rationale
3. **Reversible flag** indicating if rollback is safe
4. **Allow-drop header** (required for destructive changes)

### Standard Template

```sql
-- Brief description of the change
-- Why: Detailed explanation of why this change is needed
-- Reversible: yes|no

-- Your SQL here
CREATE TABLE example (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Track the migration
INSERT INTO schema_migrations (version) VALUES ('NNNN');
```

### Destructive Changes

For migrations that drop columns, tables, or constraints, add an explicit approval header:

```sql
-- Remove deprecated column
-- Why: The 'legacy_field' column is no longer used after feature X was removed
-- Reversible: no
-- allow-drop: Approved in PR #1234, verified data migration complete

ALTER TABLE products DROP COLUMN legacy_field;
```

## Migration Compatibility Gate

The CI pipeline includes a Migration Compatibility Gate that runs on every PR touching `migrations/`. This gate ensures:

1. **All migrations applied exactly once**: Idempotency check
2. **Data integrity**: Row counts preserved for seeded data
3. **No unauthorized destructive changes**: Drops must be explicitly approved

### Running Locally

```bash
# Run the gate with Docker
./scripts/migration-gate.sh run

# Run self-test
./scripts/migration-gate.sh self-test
```

### Environment Variables

- `TEST_DATABASE_URL`: Use an existing PostgreSQL instance instead of Docker
- `TEST_DB_PORT`: Port for the test database (default: 5433)
- `CLEANUP`: Set to `false` to keep the database container for debugging

### Gate Requirements

The gate enforces:

1. **Idempotency**: Running migrations twice must be a no-op
2. **Version tracking**: Every migration must insert into `schema_migrations`
3. **Approval for drops**: Any `DROP` statement requires `-- allow-drop:` header

## Common Patterns

### Adding a Column

```sql
-- Add email verification timestamp
-- Why: Track when users verify their email addresses
-- Reversible: yes

ALTER TABLE users 
ADD COLUMN email_verified_at TIMESTAMZ;

INSERT INTO schema_migrations (version) VALUES ('0002');
```

### Creating an Index Concurrently

```sql
-- Add index for order number lookup
-- Why: Optimize order lookup by number, which is a frequent query pattern
-- Reversible: yes

CREATE INDEX CONCURRENTLY idx_orders_number 
ON orders(number);

INSERT INTO schema_migrations (version) VALUES ('0003');
```

**Note**: `CONCURRENTLY` requires explicit transaction handling. Run in a separate transaction:

```sql
-- Add index for order number lookup
-- Why: Optimize order lookup by number
-- Reversible: yes

-- Note: CREATE INDEX CONCURRENTLY cannot run in a transaction block
-- The migration runner should handle this appropriately

INSERT INTO schema_migrations (version) VALUES ('0003');
```

### Adding a Constraint

```sql
-- Add NOT NULL constraint to order status
-- Why: Ensure all orders have a valid status
-- Reversible: yes

-- Step 1: Add constraint without validation (fast)
ALTER TABLE orders 
ADD CONSTRAINT orders_status_not_null 
CHECK (status IS NOT NULL) NOT VALID;

-- Step 2: Validate separately (in a follow-up migration or manual step)
-- This allows the constraint to be added without locking the table

INSERT INTO schema_migrations (version) VALUES ('0004');
```

### Dropping a Column (With Approval)

```sql
-- Remove deprecated 'notes' column from products
-- Why: The 'notes' column was replaced by the 'description' field in version 0005
-- Reversible: no
-- allow-drop: Approved in PR #456, verified no production data exists

ALTER TABLE products DROP COLUMN notes;

INSERT INTO schema_migrations (version) VALUES ('0006');
```

## Emergency Rollback Procedure

While kikapu uses forward-only migrations, emergencies may require schema rollback.

### Step 1: Assess Impact

Before proceeding, determine:
- What data might be lost?
- Can the change be reversed safely?
- What is the blast radius?

### Step 2: Create Rollback Migration

Create a new migration file that reverses the change:

```sql
-- Emergency rollback: restore products.notes column
-- Why: Production incident #789 - customer data was not migrated correctly
-- Reversible: no (this is a rollback)

ALTER TABLE products 
ADD COLUMN notes TEXT;

-- Note: Data restoration requires separate data migration
-- This only restores the schema structure

INSERT INTO schema_migrations (version) VALUES ('0007_rollback');
```

### Step 3: Data Recovery

If data was lost, you'll need a separate data migration to restore from backup:

1. Identify the backup timestamp
2. Extract affected data using `pg_dump` or custom queries
3. Create a data restoration migration

### Step 4: Document and Review

After the emergency:
1. Create a post-mortem document
2. Update the original migration with lessons learned
3. Add additional safeguards to prevent recurrence

## CI Integration

The Migration Compatibility Gate runs automatically in GitHub Actions. The workflow:

1. Triggers on PRs that modify `migrations/`
2. Starts a PostgreSQL service container
3. Applies migrations at the previous tag
4. Inserts fixture data
5. Applies new migrations
6. Verifies idempotency and data integrity

### Workflow Location

`.github/workflows/migrations.yml`

### Skipping the Gate

In exceptional circumstances, you can skip the gate by:
1. Adding `[skip migrations]` to the PR title (use sparingly)
2. Removing the `migrations/` path trigger temporarily

**Warning**: Skipping the gate is not recommended and should be reviewed.

## Best Practices

1. **Test migrations locally**: Run against a local PostgreSQL instance before pushing
2. **Use transactions**: Wrap DDL in transactions where possible
3. **Check locks**: Use `pg_stat_activity` to monitor for blocked queries
4. **Plan for data**: Consider data volume for large tables
5. **Document everything**: Future-you will thank present-you

## Integration with Application

The application handles migrations in two ways:

1. **Automatic mode**: Run with `--mode migrate` flag
2. **Manual mode**: Use `scripts/migration-gate.sh` for validation

The migration code is in `internal/migrate/migrate.go` and:
- Uses advisory locking (ID: `1234567890`)
- Reads migrations from the `migrations/` directory
- Tracks versions in `schema_migrations` table

## Troubleshooting

### "Migration already applied"

This is expected behavior when re-running migrations. The system correctly identifies existing migrations and skips them.

### "Failed to acquire advisory lock"

Another migration is in progress. Wait for it to complete or check:
```sql
SELECT pg_blocking_pids(pid) FROM pg_stat_activity 
WHERE query LIKE '%pg_advisory_lock%';
```

### "Relation already exists"

The migration file may have duplicate DDL. Ensure each statement is idempotent:
```sql
CREATE TABLE IF NOT EXISTS example (...);
CREATE INDEX IF NOT EXISTS idx_example ON example(column);
```

## References

- [PostgreSQL Documentation](https://www.postgresql.org/docs/current/)
- [Saleor Migration Best Practices](https://github.com/saleor/saleor)
- [AGENTS.md](AGENTS.md) - Project invariants and standards
